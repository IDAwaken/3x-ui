"""验证累计计数去重、文件续读和恢复后的统计一致性。"""
import datetime as dt
import json
from pathlib import Path
import sqlite3
import tempfile
import time
import unittest

from traffic_ingest import apply, collect, prepare, target


class TrafficTests(unittest.TestCase):
    def record(self,index,up,down):
        return {'labels':{'meter':'target-v1'},'sessionID':'test-session','recordIndex':index,'inputBytes':up,'outputBytes':down,'host':'new.example:443','network':'tcp','time':dt.datetime.now(dt.timezone.utc).isoformat(),'phase':'final' if index==3 else 'interim'}

    def test_repeated_out_of_order_and_lost_interim(self):
        with tempfile.TemporaryDirectory() as folder:
            db=prepare(Path(folder)/'traffic.db');now=int(time.time())
            with db:
                self.assertEqual(apply(db,self.record(1,100,200),now),300)
                self.assertEqual(apply(db,self.record(3,400,600),now),700)
                self.assertEqual(apply(db,self.record(3,400,600),now),0)
                self.assertEqual(apply(db,self.record(2,200,400),now),0)
                self.assertEqual(db.execute('select sum(up),sum(down) from hourly').fetchone(),(400,600))
            db.close()

    def test_partial_tail_rotation_and_restart(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);logs=root/'logs';logs.mkdir();path=root/'traffic.db';log=logs/'sessions.jsonl'
            first=json.dumps(self.record(1,100,200)).encode()+b'\n'
            second=json.dumps(self.record(2,200,400)).encode()+b'\n'
            log.write_bytes(first+second[:20])
            self.assertEqual(collect(path,logs)['新增字节'],300)
            self.assertEqual(collect(path,logs)['新增字节'],0)
            with log.open('ab') as out:out.write(second[20:])
            self.assertEqual(collect(path,logs)['新增字节'],300)
            log.rename(logs/'sessions-rotated.jsonl');log.write_text(json.dumps(self.record(3,400,600))+'\n')
            self.assertEqual(collect(path,logs)['新增字节'],400)
            self.assertEqual(collect(path,logs)['新增字节'],0)
            with sqlite3.connect(path) as db:self.assertEqual(db.execute('select sum(up),sum(down) from hourly').fetchone(),(400,600))

    def test_ip_and_domain_labels(self):
        self.assertEqual(target('EXAMPLE.COM.:443'),('example.com','example.com',443))
        self.assertEqual(target('[2001:db8::1]:53'),('[2001:db8::1]:53','2001:db8::1',53))
        self.assertEqual(target('1.1.1.1:53'),('1.1.1.1:53','1.1.1.1',53))


if __name__=='__main__':unittest.main()
