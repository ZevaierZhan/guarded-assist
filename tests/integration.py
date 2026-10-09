"""Real native Bash/HTTP/WS/SSE integration. Does not emulate shell output.
Windows executable is cross-compiled; native Windows launch is a separate test.
"""
import base64, hashlib, json, os, pathlib, re, signal, socket, subprocess, tempfile, threading, time, urllib.request, urllib.error
ROOT=pathlib.Path(__file__).resolve().parents[1]
BASE='http://127.0.0.1:18777'; ADMIN='integration-v2-private'; checks=[];frames=[]
def req(path, method='GET', data=None, token=None, origin=None):
 headers={}
 if token:headers['Authorization']='Bearer '+token
 if origin:headers['Origin']=origin
 if data is not None:headers['Content-Type']='application/json';data=json.dumps(data).encode()
 r=urllib.request.Request(BASE+path,data=data,headers=headers,method=method)
 try:
  with urllib.request.urlopen(r,timeout=5) as f:return f.status,dict(f.headers),f.read()
 except urllib.error.HTTPError as e:return e.code,dict(e.headers),e.read()
 except urllib.error.URLError:return 0,{},b'{}'
def js(*args,**kwargs):
 c,_,b=req(*args,**kwargs);return c,json.loads(b)
def check(name,yes):
 assert yes,name
 checks.append({'name':name,'status':'passed'});print('PASS',name,flush=True)
def wait(fn,timeout=12):
 end=time.time()+timeout
 while time.time()<end:
  out=fn()
  if out:return out
  time.sleep(.07)
 raise AssertionError('timeout '+repr(fn))
def state():return js('/api/requests/'+sid,token=customer)[1]
def opstate(oid):return next((o for o in state()['operations'] if o['id']==oid),{})
def createop(command,timeout=15,cwd=''):
 c,o=js('/api/requests/'+sid+'/commands','POST',{'shell':'bash','command':command,'cwd':cwd,'timeout':timeout},ADMIN);assert c==201,(c,o);return o
def approve(op):return js('/api/requests/'+sid+'/commands/'+op['id']+'/approve','POST',{'hash':op['hash']},customer)
def finish(oid):return wait(lambda:(o if o.get('status') in ['completed','failed','cancelled','denied','timed_out','unknown'] else None) if (o:=opstate(oid)) else None)
def capture_sse():
 try:
  with urllib.request.urlopen(urllib.request.Request(BASE+'/api/requests/'+sid+'/events',headers={'Authorization':'Bearer '+customer}),timeout=15) as f:
   for line in f:
    if line.startswith(b'data: '):frames.append(json.loads(line[6:]))
 except Exception:pass
server=None;native=None
try:
 log=(ROOT/'tests/server.log').open('w')
 server=subprocess.Popen([str(ROOT/'dist/assist-server-linux-amd64'),'--no-open','--admin-token',ADMIN,'--assets',str(ROOT/'dist')],stdout=log,stderr=log)
 wait(lambda:js('/health')[0]==200)
 check('service v0.2.0 identity',js('/health')[1]['version']=='0.2.0')
 check('unauthenticated creation denied',js('/api/requests','POST',{'purpose':'test'})[0]==403)
 _,s=js('/api/requests','POST',{'purpose':'真实 Shell 受控验证'},ADMIN);sid=s['id'];customer=s['invite_url'].split('&t=')[1]
 (ROOT/'tests/state-request.json').write_text(json.dumps(s,ensure_ascii=False))
 payload=s['launch_uri'].split('/')[-1];ticket=json.loads(base64.urlsafe_b64decode(payload+'='*(-len(payload)%4)))['k']
 check('support cannot approve connection',js('/api/requests/'+sid+'/accept','POST',{},ADMIN)[0]==403)
 check('bootstrap denied before web consent',js('/api/bootstrap',token=ticket)[0]==409)
 check('cross-origin consent denied',js('/api/requests/'+sid+'/accept','POST',{},customer,origin='https://bad.example')[0]==403)
 check('customer accepts connection',js('/api/requests/'+sid+'/accept','POST',{},customer)[0]==200)
 (ROOT/'tests/state-accepted.json').write_text(json.dumps(state(),ensure_ascii=False))
 code,headers,binary=req('/api/requests/'+sid+'/download?platform=linux',token=customer)
 check('per-request download preserves executable bytes',code==200 and hashlib.sha256(binary).digest()==hashlib.sha256((ROOT/'dist/assist-linux-amd64').read_bytes()).digest())
 name=re.search('filename="([^"]+)"',headers['Content-Disposition']).group(1)
 with tempfile.TemporaryDirectory(prefix='assist-v2-test-') as td:
  exe=pathlib.Path(td)/name;exe.write_bytes(binary);exe.chmod(0o700)
  native_log=(ROOT/'tests/native.log').open('w')
  native=subprocess.Popen([str(exe)],stdin=subprocess.PIPE,stdout=native_log,stderr=native_log,text=True)
  native.stdin.write('YES\n');native.stdin.flush()
  live=wait(lambda:(s if s.get('online') else None) if (s:=state()) else None)
  check('downloaded filename bootstrap pairs real process',live['device']['pid']==native.pid and live['device']['host']==socket.gethostname())
  check('real daemon reports Bash capability',live['device']['shells']==['bash'])
  check('connection does not auto-execute anything',live['operations']==[] and live['runs']==0)
  threading.Thread(target=capture_sse,daemon=True).start()
  check('customer cannot submit agent commands',js('/api/requests/'+sid+'/commands','POST',{'shell':'bash','command':'date','timeout':5},customer)[0]==403)
  check('unsupported shell refused',js('/api/requests/'+sid+'/commands','POST',{'shell':'powershell','command':'Get-Date','timeout':5},ADMIN)[0]==400)
  op=createop("printf 'stdout: hello 中文\\n'; printf 'stderr: diagnostic\\n' >&2; sleep 1; printf 'done\\n'; exit 7")
  check('proposed command remains unexecuted',opstate(op['id'])['status']=='awaiting_approval' and not opstate(op['id'])['outputs'])
  check('support cannot approve command',js('/api/requests/'+sid+'/commands/'+op['id']+'/approve','POST',{'hash':op['hash']},ADMIN)[0]==403)
  check('modified approval digest refused',js('/api/requests/'+sid+'/commands/'+op['id']+'/approve','POST',{'hash':'modified'},customer)[0]==409)
  check('customer approval sends request',approve(op)[0]==202)
  time.sleep(.25)
  check('native confirmation still required',opstate(op['id'])['status']=='awaiting_local' and not opstate(op['id'])['outputs'])
  native.stdin.write('YES\n');native.stdin.flush()
  streamed=wait(lambda:(o if o.get('status')=='running' and o.get('outputs') else None) if (o:=opstate(op['id'])) else None)
  check('real output arrives before process completion',streamed['status']=='running')
  done=finish(op['id']);joined=''.join(o['text'] for o in done['outputs'])
  check('Bash produces real UTF-8 stdout and stderr',any(o['stream']=='stdout' and '中文' in o['text'] for o in done['outputs']) and any(o['stream']=='stderr' and 'diagnostic' in o['text'] for o in done['outputs']))
  check('exit code 7 not misreported as success',done['exit_code']==7 and done['status']=='failed')
  check('SSE carries actual native output',bool(wait(lambda:any('中文' in json.dumps(f,ensure_ascii=False) for f in frames))))
  check('approval cannot be replayed',approve(op)[0]==409)
  s=state();(ROOT/'tests/state-live.json').write_text(json.dumps(s,ensure_ascii=False))
  denied=createop("printf 'SHOULD_NOT_RUN\\n'")
  check('customer can reject without sending',js('/api/requests/'+sid+'/commands/'+denied['id']+'/deny','POST',{},customer)[0]==200 and opstate(denied['id'])['outputs']==[])
  no=createop("printf 'LOCAL_REFUSAL_MUST_NOT_RUN\\n'");approve(no);time.sleep(.15);native.stdin.write('NO\n');native.stdin.flush()
  no=finish(no['id']);check('native refusal prevents execution',no['status']=='denied' and no['outputs']==[])
  cwd=createop('pwd',cwd=td);approve(cwd);time.sleep(.1);native.stdin.write('YES\n');native.stdin.flush();cwd=finish(cwd['id']);check('working directory applies to native process',td in ''.join(x['text'] for x in cwd['outputs']))
  err=createop('sleep 30',timeout=1);approve(err);time.sleep(.1);native.stdin.write('YES\n');native.stdin.flush();err=finish(err['id']);check('timeout terminates shell command',err['status']=='timed_out')
  slow=createop("printf 'running\\n'; sleep 30");approve(slow);time.sleep(.1);native.stdin.write('YES\n');native.stdin.flush();wait(lambda:opstate(slow['id'])['status']=='running')
  check('cancel is accepted',js('/api/requests/'+sid+'/commands/'+slow['id']+'/cancel','POST',{},customer)[0]==202)
  slow=finish(slow['id']);check('cancel returns native cancelled receipt',slow['status']=='cancelled')
  waitop=createop('printf hold');approve(waitop);time.sleep(.25)
  check('cancel during native consent is accepted',js('/api/requests/'+sid+'/commands/'+waitop['id']+'/cancel','POST',{},customer)[0]==202)
  check('cancelled consent helper cannot later execute',finish(waitop['id'])['status']=='cancelled')
  ok=createop("printf 'final-ok\\n'");approve(ok);time.sleep(.15);native.stdin.write('YES\n');native.stdin.flush();ok=finish(ok['id']);check('next command still works after cancellation',ok['status']=='completed' and ok['exit_code']==0)
  check('one-use pairing ticket rejected on replay',js('/api/pair','POST',{},ticket)[0]==403)
  inflight=createop("printf 'revoke-running\\n'; sleep 30");approve(inflight);time.sleep(.15);native.stdin.write('YES\n');native.stdin.flush();wait(lambda:opstate(inflight['id'])['status']=='running')
  check('revoke accepted during execution',js('/api/requests/'+sid+'/stop','POST',{},customer)[0]==200)
  native.wait(timeout=8)
  check('native exits after explicit revoke',native.returncode==0)
  check('stopped receipt not merely websocket disconnect',bool(wait(lambda:state()['stopped'])))
  check('revoke returns cancelled command before stopped receipt',opstate(inflight['id'])['status']=='cancelled')
  check('future commands denied',js('/api/requests/'+sid+'/commands','POST',{'shell':'bash','command':'date','timeout':5},ADMIN)[0]==409)
  ended=state();(ROOT/'tests/state-complete.json').write_text(json.dumps(ended,ensure_ascii=False))
 report={'scope':'real Linux native + HTTP + WS + SSE + Bash + web/native approval + cancellation','checks':checks,'not_tested':['Windows native PowerShell execution','Windows code signing and URI OS dispatch','macOS native launch','full browser network integration','cross-machine TLS relay','interactive PTY/ConPTY']}
 (ROOT/'tests/integration-results.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
 print('ALL',len(checks),'PASS',flush=True)
finally:
 if native and native.poll() is None:native.terminate();native.wait(5)
 if server:
  server.send_signal(signal.SIGINT)
  try:server.wait(4)
  except subprocess.TimeoutExpired:server.kill()
