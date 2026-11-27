"""Guarded local recovery for the single user-approved book. Default: dry run.

Requires local developer tools psutil/pymysql. Never calls a model or target site.
Backup files are generated data, not application sources. No credentials exported.
"""
import argparse
import datetime
import gzip
import hashlib
import json
import pathlib
import re

OWNER = 'choushiyiguai'
BOOK = '7705784637234105369'
VERSIONS = {'original_raw', 'original', 'ai1'}

def build_document(refs, now, source_hash):
    if set(refs) != VERSIONS or any(r.get('state') != 'ready' for r in refs.values()):
        raise ValueError('Recovery requires all three verified, ready body references')
    batch = 'recovery-' + now.replace(':', '').replace('-', '').replace('.', '')
    return {
        'bookId': BOOK,
        'bookInfo': {'workTitle': '重生八三，囤货嫁给退伍糙汉'},
        'meta': {
            'bookName': '重生八三，囤货嫁给退伍糙汉',
            'bookNameSource': 'existing_work_form',
            'status': 'pending_submit', 'queueState': 'pending_submit',
            'originalStatus': 'done', 'aiStatus': 'done',
            'siteSubmitStatus': 'pending_upload', 'manualOnly': True,
            'recoveredAt': now, 'recoverySourceHash': source_hash,
            'originalTaskDateUnknown': True, 'createdAtSource': 'recovery',
            'createdAt': now, 'currentBatchId': batch, 'batchCreatedAt': now,
            'recoveryNote': '从现有正文恢复；列表时间为恢复时间，原任务时间、分类及提交回执未知。仅手动处理。',
        },
        'bodyRefs': refs,
        'originalRawChars': refs['original_raw']['charCount'],
        'processedChars': refs['original']['charCount'],
        'versions': {'ai1': {'state': 'ready', 'charCount': refs['ai1']['charCount']}},
        'logs': [{'time': now, 'event': 'existing_bodies_recovered', 'detail': '已有完整原文、处理后原文及 AI1 已恢复；未重新生成或上传。原任务日期及回执未知，当前时间为恢复时间。'}],
        'updatedAt': now,
    }

def main():
    import psutil
    import pymysql
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backup',required=True,type=pathlib.Path)
    parser.add_argument('--apply',action='store_true')
    args=parser.parse_args()
    backup=args.backup.resolve()
    manifest_raw=(backup/'manifest.json').read_bytes()
    source_hash=hashlib.sha256(manifest_raw).hexdigest()
    manifest=json.loads(manifest_raw)
    assert manifest['owner']==OWNER and manifest['book_id']==BOOK
    expected={r['version_id']:r for r in manifest['versions']}
    assert set(expected)==VERSIONS
    for version,row in expected.items():
        raw=(backup/(version+'.txt')).read_bytes()
        assert hashlib.sha256(raw).hexdigest()==row['content_hash']==row['export_sha256']
        assert len(raw.decode('utf-8'))==row['char_count']
    pids={c.pid for c in psutil.net_connections(kind='tcp') if c.status==psutil.CONN_LISTEN and c.laddr.port==4001}
    assert len(pids)==1
    env=psutil.Process(pids.pop()).environ()
    user,password,host,port,database=re.match(r'([^:]+):(.*?)@tcp\(([^:]+):(\d+)\)/([^?]+)',env['QIANTIE_MYSQL_DSN']).groups()
    assert host in ('127.0.0.1','localhost') and database=='qiantie_go_local'
    db=pymysql.connect(host=host,port=int(port),user=user,password=password,database=database,charset='utf8mb4',cursorclass=pymysql.cursors.DictCursor,connect_timeout=5,read_timeout=15,write_timeout=15,autocommit=False)
    now=datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00','Z')
    result={'owner':OWNER,'book_id':BOOK,'mode':'apply' if args.apply else 'dry_run','versions':{}}
    try:
        with db.cursor() as cursor:
            # Lock config before membership, matching the canonical configuration writer.
            cursor.execute('SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=%s FOR UPDATE',(OWNER,))
            config_row=cursor.fetchone()
            assert config_row, 'Account config missing'
            config=json.loads(config_row['settings_json'])
            cursor.execute('SELECT document_json,updated_at FROM novel_fetch_workshop_documents WHERE owner_username=%s AND book_id=%s FOR UPDATE',(OWNER,BOOK))
            existing=cursor.fetchone()
            cursor.execute('SELECT * FROM novel_fetch_workshop_bodies WHERE owner_username=%s AND book_id=%s ORDER BY version_id FOR UPDATE',(OWNER,BOOK))
            rows=cursor.fetchall()
            assert {r['version_id'] for r in rows}==VERSIONS
            refs={}
            for row in rows:
                version=row['version_id']
                raw=gzip.decompress(row['content_blob']) if row['content_encoding']=='gzip' else bytes(row['content_blob'])
                assert hashlib.sha256(raw).hexdigest()==expected[version]['content_hash']==row['content_hash']
                assert row['char_count']==len(raw.decode('utf-8'))==expected[version]['char_count']
                assert row['revision']==expected[version]['revision']
                refs[version]={'versionId':version,'revision':row['revision'],'contentHash':row['content_hash'],'charCount':row['char_count'],'state':'ready','updatedAt':now,'lastNeededAt':now}
                result['versions'][version]={'chars':row['char_count'],'sha256':row['content_hash']}
            if existing:
                document=json.loads(existing['document_json'])
                assert document.get('meta',{}).get('recoverySourceHash')==source_hash, 'Existing task found; refuse to overwrite'
                assert document.get('bodyRefs',{}).keys()==refs.keys()
                assert all(r['state']=='ready' and r['expires_at'] is None and r['releasable_at'] is None for r in rows)
                result['already_recovered']=True
                result['batch_id']=document['meta']['currentBatchId']
                db.rollback()
            else:
                document=build_document(refs,now,source_hash)
                result['batch_id']=document['meta']['currentBatchId']
                if args.apply:
                    before={'owner':OWNER,'book_id':BOOK,'document':None,'previous_current_batch_id':config.get('novel_fetch_current_batch_id'),'previous_current_batch_at':config.get('novel_fetch_current_batch_at'),'body_metadata':[{k:v for k,v in r.items() if k!='content_blob'} for r in rows]}
                    (backup/'before-recovery.json').write_text(json.dumps(before,ensure_ascii=False,indent=2,default=str),encoding='utf-8')
                    # Plain INSERT and the unique key reject concurrent new tasks; no upsert.
                    cursor.execute('INSERT INTO novel_fetch_workshop_documents(owner_username,book_id,document_json,created_at,updated_at) VALUES(%s,%s,%s,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))',(OWNER,BOOK,json.dumps(document,ensure_ascii=False)))
                    cursor.execute("UPDATE novel_fetch_workshop_bodies SET state='ready',releasable_at=NULL,expires_at=NULL,last_needed_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE owner_username=%s AND book_id=%s",(OWNER,BOOK))
                    assert cursor.rowcount==3
                    config['novel_fetch_current_batch_id']=result['batch_id']
                    config['novel_fetch_current_batch_at']=now
                    cursor.execute('UPDATE novel_fetch_workshop_configs SET settings_json=%s WHERE owner_username=%s',(json.dumps(config,ensure_ascii=False),OWNER))
                    db.commit()
                    result['applied']=True
                else:
                    db.rollback()
        (backup/('recovery-'+('apply' if args.apply else 'dry-run')+'.json')).write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(result,ensure_ascii=False))
    except Exception:
        db.rollback()
        raise
    finally:
        db.close()

if __name__=='__main__': main()
