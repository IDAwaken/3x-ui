package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSiteTrafficRangeAndBeijingDay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "traffic.db")
	db, err := sql.Open("sqlite3", p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE hourly(hour INTEGER,site TEXT,up INTEGER,down INTEGER); CREATE TABLE metadata(key TEXT,value TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 4, 16, 30, 0, 0, time.UTC)
	start := (now.Unix() - 43200*60) / 3600 * 3600
	_, err = db.Exec("INSERT INTO metadata VALUES('started',?),('last_sample',?)", start, now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		h    int64
		s    string
		u, d int64
	}{
		{start - 3600, "excluded.example", 999, 999},
		{now.Add(-time.Hour).Truncate(time.Hour).Unix(), "a.example", 10, 20},
		{now.Truncate(time.Hour).Unix(), "a.example", 30, 40},
		{now.Truncate(time.Hour).Unix(), "其他", 100, 200},
	} {
		if _, err = db.Exec("INSERT INTO hourly VALUES(?,?,?,?)", r.h, r.s, r.u, r.d); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	got, err := readSiteTraffic(context.Background(), p, 43200, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Up != 140 || got.Down != 260 || len(got.Rows) != 2 || got.Rows[0].Site != "其他" {
		t.Fatalf("汇总或排序错误: %+v", got)
	}
	points := got.Rows[1].Points
	a, b := points[len(points)-2], points[len(points)-1]
	if a.Up+a.Down != 30 || b.Up+b.Down != 70 {
		t.Fatalf("北京时间跨日划分错误: %+v %+v", a, b)
	}
	if got.Start != start || got.LastSample != now.Unix() {
		t.Fatalf("范围或采样时间错误: %+v", got)
	}
}

func TestSiteTrafficMissingDatabaseIsNotCreated(t *testing.T) {
	_, err := readSiteTraffic(context.Background(), filepath.Join(t.TempDir(), "missing.db"), 43200, time.Now())
	if err == nil {
		t.Fatal("缺失数据库必须报错，不得创建空库")
	}
}

func TestThirtyDayMetricTier(t *testing.T) {
	s := newSeries()
	tier := s.pickTier(30 * 86400)
	if tier.resolution != 3600 || tier.capacity != 720 {
		t.Fatalf("三十天保留层错误: %+v", tier)
	}
	if !IsAllowedHistoryBucket(43200) {
		t.Fatal("三十天筛选未放行")
	}
}

func TestThirtyDayTierRestoresExistingWeek(t *testing.T) {
	h := newMetricHistory()
	now := time.Now().Unix()
	h.restore(map[string]persistedSeries{"cpu": {Tiers: []persistedTier{{Resolution: 600, Samples: []MetricSample{{T: now - 7200, V: 20}, {T: now - 6600, V: 40}, {T: now - 3600, V: 60}}}}}})
	out := h.aggregate("cpu", 43200, 60)
	if len(out) == 0 {
		t.Fatal("新增月度层必须保留升级前的七天历史")
	}
}

func TestSiteFilterAndFullStart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "traffic.db")
	db, err := sql.Open("sqlite3", p)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	hour := now.Unix() / 3600 * 3600
	_, err = db.Exec("CREATE TABLE hourly(hour INTEGER,site TEXT,up INTEGER,down INTEGER); CREATE TABLE metadata(key TEXT,value TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO hourly VALUES(?,?,10,20),(?,?,30,40)", hour, "a.example", hour, "b.example")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("CREATE TABLE target_hourly(hour INTEGER,host TEXT,port INTEGER,network TEXT,up INTEGER,down INTEGER)")
	db.Exec("INSERT INTO target_hourly VALUES(?,?,443,'tcp',10,20),(?,?,443,'tcp',30,40)", hour, "a.example", hour, "b.example")
	db.Exec("INSERT INTO metadata VALUES('full_started',?)", now.Unix()-10)
	db.Close()
	got, err := readSiteTraffic(context.Background(), p, 43200, now, "b.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Site != "b.example" || got.Up != 30 || got.Down != 40 || got.FullStarted != now.Unix()-10 {
		t.Fatalf("指定域名过滤错误: %+v", got)
	}
	empty, err := readSiteTraffic(context.Background(), p, 43200, now, "' OR 1=1 --")
	if err != nil || len(empty.Rows) != 0 {
		t.Fatal("查询条件未参数化")
	}
}
