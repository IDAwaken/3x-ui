package service

import (
	"context"
	"database/sql"
	"net"
	"sort"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SiteTrafficPoint struct {
	T    int64 `json:"t"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type SiteTrafficRow struct {
	Site   string             `json:"site"`
	Up     int64              `json:"up"`
	Down   int64              `json:"down"`
	Total  int64              `json:"total"`
	Points []SiteTrafficPoint `json:"points"`
}

type SiteTrafficHistory struct {
	Start       int64              `json:"start"`
	End         int64              `json:"end"`
	Started     int64              `json:"started"`
	LastSample  int64              `json:"lastSample"`
	FullStarted int64              `json:"fullStarted,omitempty"`
	Up          int64              `json:"up"`
	Down        int64              `json:"down"`
	Rows        []SiteTrafficRow   `json:"rows"`
	Points      []SiteTrafficPoint `json:"points"`
}

func GetSiteTrafficHistory(ctx context.Context, bucket int, site string) (*SiteTrafficHistory, error) {
	return readSiteTraffic(ctx, "/var/lib/3x-ui-sites/traffic.db", bucket, time.Now(), site)
}

func readSiteTraffic(ctx context.Context, path string, bucket int, now time.Time, filter ...string) (*SiteTrafficHistory, error) {
	// 只读独立统计库，不触碰面板配置；小时粒度与采集器保持一致。
	start := (now.Unix() - int64(bucket)*60) / 3600 * 3600
	result := &SiteTrafficHistory{Start: start, End: now.Unix(), Rows: []SiteTrafficRow{}, Points: []SiteTrafficPoint{}}
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&_query_only=1&_busy_timeout=3000")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var started, last, full int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(CASE WHEN key='started' THEN CAST(value AS INTEGER) END),0),COALESCE(MAX(CASE WHEN key='last_sample' THEN CAST(value AS INTEGER) END),0),COALESCE(MAX(CASE WHEN key='full_started' THEN CAST(value AS INTEGER) END),0) FROM metadata").Scan(&started, &last, &full); err != nil {
		return nil, err
	}
	result.FullStarted = full
	result.Started = started
	if full > 0 {
		result.Started = full
	}
	result.LastSample = last
	query := "SELECT hour,site,0,up,down FROM hourly WHERE hour>=? AND hour<?"
	args := []any{start, now.Unix()}
	if full > 0 {
		query = "SELECT hour,host,port,SUM(up),SUM(down) FROM target_hourly WHERE hour>=? AND hour<?"
	}
	if len(filter) > 0 && filter[0] != "" {
		if full > 0 {
			name := filter[0]
			if host, port, err := net.SplitHostPort(name); err == nil && net.ParseIP(host) != nil {
				query += " AND host=? AND port=?"
				args = append(args, host, port)
			} else {
				query += " AND host=?"
				args = append(args, name)
			}
		} else {
			query += " AND site=?"
			args = append(args, filter[0])
		}
	}
	if full > 0 {
		query += " GROUP BY hour,host,port ORDER BY hour,host"
	} else {
		query += " ORDER BY hour,site"
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	loc := time.FixedZone("北京时间", 8*3600)
	step := int64(3600)
	if bucket >= 1440 {
		step = 86400
	}
	aligned := func(t int64) int64 {
		if step == 86400 {
			d := time.Unix(t, 0).In(loc)
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc).Unix()
		}
		return t / 3600 * 3600
	}
	perSite := map[string]*SiteTrafficRow{}
	perPoint := map[string]map[int64]*SiteTrafficPoint{}
	totalPoints := map[int64]*SiteTrafficPoint{}
	for rows.Next() {
		var h, up, down int64
		var name string
		var port int
		if err = rows.Scan(&h, &name, &port, &up, &down); err != nil {
			return nil, err
		}
		if full > 0 && net.ParseIP(name) != nil {
			name = net.JoinHostPort(name, strconv.Itoa(port))
		}
		if perSite[name] == nil {
			perSite[name] = &SiteTrafficRow{Site: name}
			perPoint[name] = map[int64]*SiteTrafficPoint{}
		}
		r := perSite[name]
		r.Up += up
		r.Down += down
		r.Total += up + down
		t := aligned(h)
		if len(perSite) <= 100 {
			if perPoint[name][t] == nil {
				perPoint[name][t] = &SiteTrafficPoint{T: t}
			}
			p := perPoint[name][t]
			p.Up += up
			p.Down += down
		}
		if totalPoints[t] == nil {
			totalPoints[t] = &SiteTrafficPoint{T: t}
		}
		totalPoints[t].Up += up
		totalPoints[t].Down += down
		result.Up += up
		result.Down += down
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	first := start
	if result.Started > first {
		first = result.Started
	}
	// 仅在已有监控覆盖的时间内补零，不伪造启用前的历史。
	for t := aligned(first); t <= aligned(now.Unix()) && first > 0; t += step {
		p := totalPoints[t]
		if p == nil {
			p = &SiteTrafficPoint{T: t}
		}
		result.Points = append(result.Points, *p)
		if len(perSite) > 100 {
			continue
		}
		for name, r := range perSite {
			p := perPoint[name][t]
			if p == nil {
				p = &SiteTrafficPoint{T: t}
			}
			r.Points = append(r.Points, *p)
		}
	}
	for _, r := range perSite {
		if r.Total > 0 {
			result.Rows = append(result.Rows, *r)
		}
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		if a.Total == b.Total {
			return a.Site < b.Site
		}
		return a.Total > b.Total
	})
	return result, nil
}
