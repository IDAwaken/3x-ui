#!/usr/bin/env python3
"""将逐目标连接累计计数去重后按小时写入现有流量数据库。"""
import argparse
import datetime as dt
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import sqlite3
import time


def prepare(path):
    path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    os.chmod(path.parent,0o700)
    db=sqlite3.connect(path,timeout=20)
    os.chmod(path,0o600)
    db.executescript('''
      CREATE TABLE IF NOT EXISTS hourly(hour INTEGER,site TEXT,up INTEGER,down INTEGER,PRIMARY KEY(hour,site));
      CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT);
      CREATE TABLE IF NOT EXISTS meter_sessions(id TEXT PRIMARY KEY,idx INTEGER,up INTEGER,down INTEGER,updated INTEGER,finished INTEGER);
      CREATE TABLE IF NOT EXISTS meter_files(id TEXT PRIMARY KEY,offset INTEGER,updated INTEGER);
      CREATE TABLE IF NOT EXISTS target_hourly(hour INTEGER,host TEXT,port INTEGER,network TEXT,up INTEGER,down INTEGER,PRIMARY KEY(hour,host,port,network));
    ''')
    return db


def target(host):
    if not isinstance(host,str) or ':' not in host:raise ValueError('无效目标')
    name,port=host.rsplit(':',1);name=name.strip('[]').lower().rstrip('.')
    port=int(port)
    if not name or not 0<port<=65535:raise ValueError('无效目标端口')
    try:
        ip=ipaddress.ip_address(name)
        site=('['+ip.compressed+']' if ip.version==6 else ip.compressed)+':'+str(port)
        return site,name,port
    except ValueError:
        name=name.encode('idna').decode('ascii')
        if len(name)>253 or any(not(x.isalnum() or x in '.-_') for x in name):raise ValueError('无效域名')
        return name,name,port


def apply(db,row,now):
    if row.get('labels',{}).get('meter')!='target-v1':return 0
    sid=row['sessionID'];idx=int(row['recordIndex']);up=int(row['inputBytes']);down=int(row['outputBytes'])
    if not isinstance(sid,str) or not sid or len(sid)>128 or idx<1 or min(up,down)<0:raise ValueError('无效会话计数')
    network=row['network']
    if network not in ('tcp','udp'):raise ValueError('无效传输类型')
    site,host,port=target(row['host'])
    stamp=int(dt.datetime.fromisoformat(row['time'].replace('Z','+00:00')).timestamp())
    if stamp<now-90*86400 or stamp>now+300:raise ValueError('记录时间超出允许范围')
    old=db.execute('select idx,up,down from meter_sessions where id=?',(sid,)).fetchone()
    if old and idx<=old[0]:return 0
    if old and (up<old[1] or down<old[2]):raise ValueError('同一会话累计计数倒退')
    delta_up=up-(old[1] if old else 0);delta_down=down-(old[2] if old else 0)
    hour=stamp//3600*3600
    if delta_up or delta_down:
        db.execute('''insert into hourly values(?,?,?,?) on conflict(hour,site) do update set up=up+excluded.up,down=down+excluded.down''',(hour,site,delta_up,delta_down))
        db.execute('''insert into target_hourly values(?,?,?,?,?,?) on conflict(hour,host,port,network) do update set up=up+excluded.up,down=down+excluded.down''',(hour,host,port,network,delta_up,delta_down))
    db.execute('insert or replace into meter_sessions values(?,?,?,?,?,?)',(sid,idx,up,down,stamp,int(row.get('phase')=='final')))
    return delta_up+delta_down


def collect(path,folder):
    now=int(time.time());records=0;total=0
    with prepare(path) as db:
        db.execute('BEGIN IMMEDIATE')
        for p in sorted(folder.glob('sessions*.jsonl'),key=lambda p:p.stat().st_mtime):
            with p.open('rb') as source:
                first=source.readline(1048577)
                if not first.endswith(b'\n'):continue
                stat=os.fstat(source.fileno())
                key=str(stat.st_dev)+':'+str(stat.st_ino)+':'+hashlib.sha256(first).hexdigest()
                old=db.execute('select offset from meter_files where id=?',(key,)).fetchone()
                offset=old[0] if old else 0
                if offset>stat.st_size:raise RuntimeError('记录文件意外缩短')
                source.seek(offset)
                while True:
                    line=source.readline(1048577)
                    if not line:break
                    if not line.endswith(b'\n'):
                        if len(line)>1048576:raise RuntimeError('记录超过大小限制')
                        break
                    row=json.loads(line)
                    total+=apply(db,row,now);records+=1;offset=source.tell()
                db.execute('insert or replace into meter_files values(?,?,?)',(key,offset,now))
        db.execute('insert or ignore into metadata values(?,?)',('started',str(now)))
        db.execute('insert or replace into metadata values(?,?)',('last_sample',str(now)))
        for table in ('hourly','target_hourly'):db.execute('delete from '+table+' where hour<?',(now-90*86400,))
        db.execute('delete from meter_sessions where finished=1 and updated<?',(now-7*86400,))
        db.execute('delete from meter_files where updated<?',(now-90*86400,))
    return {'处理记录':records,'新增字节':total,'采集时间':dt.datetime.fromtimestamp(now,dt.timezone(dt.timedelta(hours=8))).isoformat()}


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--db',type=Path,default=Path('/var/lib/3x-ui-sites/traffic.db'))
    p.add_argument('--logs',type=Path,default=Path('/var/lib/3x-ui-meter'))
    args=p.parse_args()
    print(json.dumps(collect(args.db,args.logs),ensure_ascii=False,indent=2))


if __name__=='__main__':main()
