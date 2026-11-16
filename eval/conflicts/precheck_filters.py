import json,sqlite3,sys,statistics,re
sys.path.insert(0,'eval/conflicts')
from kuturns import ku_turns
d={q['question_id']:q for q in json.load(open('eval/lme/data/longmemeval_s_cleaned.json'))}
S=json.load(open('eval/conflicts/sets.json'))['development']
ids=S['knowledge_update']+S['other']
def ents(t):
    out=set(); start=True
    for w in re.findall(r"[A-Za-z][A-Za-z'’-]+|[.!?:;\n]",t):
        if w in '.!?:;\n': start=True; continue
        if w[0].isupper() and len(w)>1 and (not start or w.isupper()): out.add(w.lower().rstrip("'s"))
        start=False
    return out
F={'all':lambda p,r,t:True,'user-user':lambda p,r,t:r[p['older_id']]=='user' and r[p['newer_id']]=='user',
   'shared entity':lambda p,r,t:bool(ents(t[p['older_id']])&ents(t[p['newer_id']])),
   'user-user + shared entity':lambda p,r,t:r[p['older_id']]=='user' and r[p['newer_id']]=='user' and bool(ents(t[p['older_id']])&ents(t[p['newer_id']]))}
for mc in ('0.80','0.85'):
  res={k:[[],0,0] for k in F}
  for qid in ids:
    w=f'eval/conflicts/work/dev/{qid}'
    sc=json.load(open(f'{w}/scan-{mc}.json'))['output']['scan']['candidates']
    db=sqlite3.connect(f'{w}/base/chanda.db')
    role={i:r for i,r in db.execute('select id, role from events')}; text={i:c for i,c in db.execute('select id, content from events')}
    st=json.load(open(f'{w}/base/study.json')); tm={t['key']:t['event_id'] for t in st['turns']}
    k=ku_turns(d[qid]); pair=None
    if k: pair={tm.get(f"s{k[0][0]}t{k[0][1]}"),tm.get(f"s{k[1][0]}t{k[1][1]}")}
    for name,f in F.items():
      kept=[p for p in sc if f(p,role,text)]
      res[name][0].append(len(kept))
      if pair: res[name][1]+=1; res[name][2]+=any({p['older_id'],p['newer_id']}==pair for p in kept)
  for name,(n,q,h) in res.items(): print(mc,f'{name:28s} mean {statistics.mean(n):7.1f} median {statistics.median(n):6.0f}  KU pair reached {h}/{q}')
