import { useEffect, useState } from 'react';
import { Alert, Collapse, Empty, Progress, Select, Spin, Table, Tooltip, Typography } from 'antd';
import { InfoCircleOutlined } from '@ant-design/icons';
import { HttpUtil, SizeFormatter } from '@/utils';
import { Sparkline } from '@/components/viz';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import './SiteTrafficHistory.css';

interface Point {
  t: number;
  up: number;
  down: number;
}
interface Row {
  site: string;
  up: number;
  down: number;
  total: number;
  points: Point[];
}
interface History {
  start: number;
  end: number;
  started: number;
  lastSample: number;
  fullStarted?: number;
  up: number;
  down: number;
  rows: Row[];
  points: Point[];
}
const size = (n: number) => SizeFormatter.sizeFormat(n);
const fullDate = (n: number) =>
  n
    ? new Date(n * 1000).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
    : '暂无数据';
const shortDate = (n: number) =>
  new Date(n * 1000).toLocaleDateString('zh-CN', {
    timeZone: 'Asia/Shanghai',
    month: '2-digit',
    day: '2-digit',
  });
const shortTime = (n: number) =>
  n
    ? new Date(n * 1000).toLocaleTimeString('zh-CN', {
        timeZone: 'Asia/Shanghai',
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
      })
    : '暂无';

export default function SiteTrafficHistory({ bucket }: { bucket: number }) {
  const { isMobile } = useMediaQuery();
  const [history, setHistory] = useState<History | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState('');
  const [expanded, setExpanded] = useState<string[]>([]);
  const [trend, setTrend] = useState<{ site: string; bucket: number; points: Point[] } | null>(
    null,
  );
  const trendOpen = expanded.includes('trend');
  useEffect(() => {
    let cancelled = false;
    let initial = true;
    async function load() {
      await Promise.resolve();
      if (cancelled) return;
      if (initial) {
        setHistory(null);
        setBusy(true);
        setError('');
        initial = false;
      }
      try {
        const msg = await HttpUtil.get<History>(`/panel/api/server/siteTraffic/${bucket}`);
        if (cancelled) return;
        if (!msg?.success || !msg.obj) throw new Error('流量数据暂不可用');
        setHistory(msg.obj);
        setError('');
      } catch {
        if (!cancelled) setError('读取失败，请稍后重试。');
      } finally {
        if (!cancelled) setBusy(false);
      }
    }
    void load();
    const timer = window.setInterval(() => void load(), 60000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [bucket]);
  useEffect(() => {
    let cancelled = false;
    if (!selected || !trendOpen) return;
    void HttpUtil.get<History>(
      `/panel/api/server/siteTraffic/${bucket}?site=${encodeURIComponent(selected)}`,
    ).then((msg) => {
      if (!cancelled && msg?.success && msg.obj)
        setTrend({ site: selected, bucket, points: msg.obj.points });
    });
    return () => {
      cancelled = true;
    };
  }, [selected, bucket, trendOpen, history?.end]);
  const row = history?.rows.find((r) => r.site === selected);
  const points = selected
    ? trend?.site === selected && trend.bucket === bucket
      ? trend.points
      : (row?.points ?? [])
    : (history?.points ?? []);
  const total = (history?.up ?? 0) + (history?.down ?? 0);
  const percent = (value: number) => (total ? (value / total) * 100 : 0);
  return (
    <div className="site-history">
      {error && <Alert type="error" showIcon title={error} />}
      <Spin spinning={busy}>
        {history && (
          <>
            <div className="site-history-meta">
              <Typography.Text type="secondary">
                {shortDate(history.start)}—{shortDate(history.end)}
                {history.started > history.start ? ` · ${shortDate(history.started)}起` : ''}
              </Typography.Text>
              <Typography.Text type="secondary">
                更新 {shortTime(history.lastSample)}{' '}
                <Tooltip
                  title={`北京时间；范围：${fullDate(history.start)} 至 ${fullDate(history.end)}；监控启用：${fullDate(history.started)}。${history.fullStarted ? `全量统计自 ${fullDate(history.fullStarted)}；此前为旧分类统计。` : ''}上传＋下载，小时粒度。`}
                >
                  <InfoCircleOutlined aria-label="统计说明" />
                </Tooltip>
              </Typography.Text>
            </div>
            {history.lastSample > 0 && history.end - history.lastSample > 180 && (
              <Alert type="warning" showIcon title="采集暂未更新" />
            )}
            <div className="site-history-totals">
              {[
                ['合计', total],
                ['上传', history.up],
                ['下载', history.down],
              ].map(([label, value]) => (
                <div key={label}>
                  <Typography.Text type="secondary">{label}</Typography.Text>
                  <strong>{size(Number(value))}</strong>
                </div>
              ))}
            </div>
            {history.rows.length ? (
              <>
                <Table<Row>
                  rowKey="site"
                  size="small"
                  dataSource={history.rows}
                  scroll={{ x: isMobile ? 300 : 560 }}
                  pagination={{ pageSize: 10, hideOnSinglePage: true, showSizeChanger: false }}
                  columns={[
                    {
                      title: '网站',
                      dataIndex: 'site',
                      width: isMobile ? 110 : 160,
                      render: (name: string) => (
                        <Tooltip title={name}>
                          <Typography.Link
                            aria-label={name}
                            onClick={() => {
                              setSelected(name);
                              setExpanded(['trend']);
                            }}
                          >
                            {name}
                          </Typography.Link>
                        </Tooltip>
                      ),
                    },
                    { title: '上传', dataIndex: 'up', render: size, responsive: ['md'] },
                    { title: '下载', dataIndex: 'down', render: size, responsive: ['md'] },
                    {
                      title: '合计',
                      dataIndex: 'total',
                      width: 100,
                      render: (value: number, record) => (
                        <Tooltip title={`上传 ${size(record.up)} · 下载 ${size(record.down)}`}>
                          {size(value)}
                        </Tooltip>
                      ),
                      sorter: (a, b) => a.total - b.total,
                      defaultSortOrder: 'descend',
                    },
                    {
                      title: '占比',
                      width: isMobile ? 90 : 120,
                      render: (_, r) => (
                        <div className="site-history-share">
                          <span>{percent(r.total).toFixed(1)}%</span>
                          <Progress percent={percent(r.total)} showInfo={false} size="small" />
                        </div>
                      ),
                    },
                  ]}
                />
                <Collapse
                  ghost
                  activeKey={expanded}
                  onChange={(keys) => setExpanded(Array.isArray(keys) ? keys : [keys])}
                  items={[
                    {
                      key: 'trend',
                      label: '趋势',
                      children: (
                        <div className="cpu-chart-wrap">
                          <div className="site-history-chart-heading">
                            <span className="history-chart-title">
                              {bucket >= 1440 ? '每日' : '每小时'}
                            </span>
                            <Select
                              aria-label="选择趋势网站"
                              value={row ? selected : ''}
                              onChange={setSelected}
                              showSearch
                              optionFilterProp="label"
                              options={[
                                { value: '', label: '全部' },
                                ...history.rows.map((r) => ({
                                  value: r.site,
                                  label: r.site,
                                })),
                              ]}
                            />
                          </div>
                          {points.length === 1 ? (
                            <>
                              <Typography.Paragraph type="secondary">
                                仅有当日数据
                              </Typography.Paragraph>
                              {(
                                [
                                  ['上传', points[0].up, '#1890ff'],
                                  ['下载', points[0].down, '#13c2c2'],
                                ] as const
                              ).map(([label, value, color]) => (
                                <div className="site-history-bar" key={label}>
                                  <div>
                                    <span>{label}</span>
                                    <span>{size(value)}</span>
                                  </div>
                                  <Progress
                                    percent={
                                      (value / Math.max(points[0].up, points[0].down, 1)) * 100
                                    }
                                    strokeColor={color}
                                    showInfo={false}
                                  />
                                </div>
                              ))}
                            </>
                          ) : (
                            <Sparkline
                              data={points.map((p) => p.up)}
                              data2={points.map((p) => p.down)}
                              name1="上传"
                              name2="下载"
                              labels={points.map((p) => fullDate(p.t))}
                              height={200}
                              maxPoints={points.length || 1}
                              stroke="#1890ff"
                              stroke2="#13c2c2"
                              showAxes
                              showGrid
                              showTooltip
                              showLegend
                              tickCountX={5}
                              valueMin={0}
                              valueMax={null}
                              yFormatter={size}
                            />
                          )}
                        </div>
                      ),
                    },
                  ]}
                />
              </>
            ) : (
              <Empty description="暂无流量" />
            )}
          </>
        )}
      </Spin>
    </div>
  );
}
