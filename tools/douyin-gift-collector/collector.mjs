// BiliPDJ-GO optional Douyin browser collector. Node.js >= 22; no npm deps.
// Uses an isolated persistent Chrome/Edge profile and the Go localhost frame API.
// Modes: --login (visible QR window) or --run (headless multi-room monitor).
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const root=path.dirname(fileURLToPath(import.meta.url));
const argv=process.argv.slice(2);
function arg(key,otherwise='') {
  const i=argv.indexOf('--'+key);
  return i>=0 && argv[i+1] && !argv[i+1].startsWith('--') ? argv[i+1] : otherwise;
}
const loginMode=argv.includes('--login');
const profile=path.resolve(arg('profile',path.join(os.homedir(),'.bilipdj-douyin')));
const statusFile=path.resolve(arg('status',path.join(profile,'..','douyin-collector-status.json')));
const stopFile=path.resolve(arg('stop-file',path.join(profile,'..','douyin-collector-stop')));
const debuggerPort=Number(arg('debug-port','9225'));
const port=Number(arg('go-port','9816'));
const token=process.env.PDJ_DOUYIN_BRIDGE_TOKEN || '';
const rooms=arg('rooms','').split(',').filter(Boolean).map(s=>{
  const split=s.split(':');
  const id=split[0], instance=split[1]||'douyin';
  if(!/^[0-9A-Za-z_-]{1,100}$/.test(id) || !/^(douyin|douyin-[a-z0-9-]{3,52})$/.test(instance)){
    throw new Error('invalid room mapping');
  }
  return {id,instance};
});
if(!Number.isInteger(debuggerPort)||debuggerPort<1024||debuggerPort>65535)throw new Error('bad debugger port');
if(!Number.isInteger(port)||port<1||port>65535)throw new Error('bad Go port');
if(!loginMode && (!rooms.length || !token || token.length<20))throw new Error('rooms and secure Go pairing token required');

const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let browser=null, cdp=null, closing=false;
const state={mode:loginMode?'login':'collector',login_state:'unknown',running:true,started_at:new Date().toISOString(),rooms:{},last_error:''};
function report(partial={}) {
  Object.assign(state,partial);
  state.updated_at=new Date().toISOString();
  const tmp=statusFile+'.tmp';
  try {
    fs.mkdirSync(path.dirname(statusFile),{recursive:true,mode:0o700});
    fs.writeFileSync(tmp,JSON.stringify(state,null,2),{mode:0o600});
    fs.renameSync(tmp,statusFile);
  } catch(e) {console.error('[douyin collector] cannot save status: '+e.message);}
}
function logError(e) {
  const text=String(e?.message||e).slice(0,300);
  console.error('[douyin collector]',text);
  report({last_error:text});
}
function findChrome() {
  const explicit=arg('chrome',process.env.PDJ_DOUYIN_CHROME||'');
  const candidates=[
    explicit,
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    '/usr/bin/google-chrome','/usr/bin/google-chrome-stable',
    '/usr/bin/chromium','/usr/bin/chromium-browser',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge'
  ];
  const which=(name)=>{
    for(const folder of (process.env.PATH||'').split(path.delimiter)){
      const file=path.join(folder,name);
      if(fs.existsSync(file))return file;
    }
    return '';
  };
  for(const x of candidates)if(x && fs.existsSync(x) && fs.statSync(x).isFile())return x;
  return which('chrome')||which('chromium')||which('google-chrome')||which('msedge')||'';
}
async function version() {
  try {
    const r=await fetch('http://127.0.0.1:'+debuggerPort+'/json/version',
      {signal:AbortSignal.timeout(1200)});
    if(r.ok)return await r.json();
  }catch{}
  return null;
}
async function bootChrome(headless) {
  if(await version())throw new Error('debug port already in use; use an unused port');
  const chrome=findChrome();
  if(!chrome)throw new Error('Chrome/Edge not found: install browser or set PDJ_DOUYIN_CHROME');
  fs.mkdirSync(profile,{recursive:true,mode:0o700});
  const args=[
    '--remote-debugging-address=127.0.0.1',
    '--remote-debugging-port='+debuggerPort,
    '--user-data-dir='+profile,
    '--no-first-run','--no-default-browser-check','--mute-audio',
    '--disable-background-timer-throttling',
    '--disable-backgrounding-occluded-windows',
    '--disable-renderer-backgrounding',
    '--disable-features=Translate,MediaRouter,CalculateNativeWinOcclusion',
    '--window-size=1100,780'
  ];
  if(headless)args.push('--headless=new','--disable-gpu');
  args.push('about:blank');
  browser=spawn(chrome,args,{stdio:'ignore',windowsHide:headless});
  browser.on('error',e=>logError(e));
  for(let i=0;i<50;i++){
    if(closing)throw new Error('stopping');
    await sleep(250);
    if(await version())return;
    if(browser.exitCode!==null)throw new Error('browser closed with code '+browser.exitCode);
  }
  throw new Error('Chrome debug endpoint startup timeout');
}
class CDP {
  constructor(address) {this.address=address;this.id=0;this.pending=new Map();this.closed=false;}
  async connect() {
    const ws=new WebSocket(this.address);
    this.ws=ws;
    await new Promise((resolve,reject)=>{
      const timeout=setTimeout(()=>reject(new Error('CDP open timeout')),8000);
      ws.addEventListener('open',()=>{clearTimeout(timeout);resolve();},{once:true});
      ws.addEventListener('error',()=>{clearTimeout(timeout);reject(new Error('CDP open error'));},{once:true});
    });
    ws.addEventListener('message',(event)=>{
      let msg;try{msg=JSON.parse(event.data)}catch{return;}
      if(msg.id && this.pending.has(msg.id)){
        const entry=this.pending.get(msg.id); this.pending.delete(msg.id);
        clearTimeout(entry.timer);
        if(msg.error)entry.reject(new Error(entry.method+' '+msg.error.message));
        else entry.resolve(msg.result||{});
      }
    });
    ws.addEventListener('close',()=>{
      this.closed=true;
      for(const entry of this.pending.values()) {
        clearTimeout(entry.timer);entry.reject(new Error('CDP closed'));
      }
      this.pending.clear();
    });
    return this;
  }
  send(method,params={},sessionId='',timeoutMs=15000) {
    if(this.closed) return Promise.reject(new Error('CDP closed'));
    const id=++this.id;
    return new Promise((resolve,reject)=>{
      const timer=setTimeout(()=>{this.pending.delete(id);reject(new Error(method+' timeout'));},timeoutMs);
      this.pending.set(id,{method,timer,resolve,reject});
      try {
        this.ws.send(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})}));
      } catch(e) {clearTimeout(timer);this.pending.delete(id);reject(e);}
    });
  }
  close(){try{this.ws?.close()}catch{}}
}
async function connectCDP(){
  const v=await version();if(!v?.webSocketDebuggerUrl)throw new Error('CDP not ready');
  return new CDP(v.webSocketDebuggerUrl).connect();
}
async function getLoginStatus(sessionId='') {
  let cookies;
  try {
    const result=await cdp.send('Storage.getCookies',{},sessionId);
    cookies=result.cookies;
  }catch{
    const result=await cdp.send('Network.getAllCookies',{},sessionId);
    cookies=result.cookies;
  }
  const now=Date.now()/1000;
  return Array.isArray(cookies) && cookies.some(c=>
    /(^|\.)douyin\.com$/.test(String(c.domain).replace(/^\./,'')) &&
    ['sessionid','sessionid_ss'].includes(c.name) &&
    !!c.value && (!c.expires || c.expires<0 || c.expires>now));
}
async function closeBrowser(){
  try{await cdp?.send('Browser.close',{},'',2500);}catch{}
  try{cdp?.close()}catch{}
  if(browser && browser.exitCode===null){try{browser.kill()}catch{}}
}
async function stop(reason='stopped'){
  if(closing)return;
  closing=true;
  state.running=false;state.stop_reason=reason;
  report();
  await closeBrowser();
}
process.on('SIGINT',()=>{void stop('signal').finally(()=>process.exit(0));});
process.on('SIGTERM',()=>{void stop('signal').finally(()=>process.exit(0));});
async function inspectStop(){
  if(fs.existsSync(stopFile)) {
    try{fs.unlinkSync(stopFile)}catch{}
    await stop('requested');
    return true;
  }
  return false;
}
async function logIn(){
  await bootChrome(false);
  cdp=await connectCDP();
  await cdp.send('Target.createTarget',{url:'https://live.douyin.com/'});
  report({login_state:'waiting_for_scan'});
  for(let i=0;i<150;i++){
    if(await inspectStop())return;
    try {
      if(await getLoginStatus()){
        report({login_state:'cookie_detected',logged_in:true,checked_at:new Date().toISOString()});
        await stop('login_complete');
        return;
      }
    }catch(e){logError(e);}
    await sleep(2000);
  }
  report({login_state:'timed_out',logged_in:false});
  await stop('login_timeout');
}

const hookSource=fs.readFileSync(path.join(root,'hook.js'),'utf8');
class Room {
  constructor(id,instance){this.id=id;this.instance=instance;this.frames=0;this.sent=0;this.errors=0;this.targetId='';this.sessionId='';this.lastIM=0;this.lastReload=Date.now();this.title='';}
  async open(){
    const t=await cdp.send('Target.createTarget',{url:'about:blank'});
    this.targetId=t.targetId;
    const attach=await cdp.send('Target.attachToTarget',{targetId:t.targetId,flatten:true});
    this.sessionId=attach.sessionId;
    await cdp.send('Page.enable',{},this.sessionId);
    await cdp.send('Runtime.enable',{},this.sessionId);
    await cdp.send('Page.addScriptToEvaluateOnNewDocument',{source:hookSource},this.sessionId);
    await cdp.send('Page.navigate',{url:'https://live.douyin.com/'+this.id},this.sessionId,45000);
    this.lastReload=Date.now();
  }
  async close(){if(this.targetId)try{await cdp.send('Target.closeTarget',{targetId:this.targetId})}catch{};this.targetId='';this.sessionId='';}
  async reload(){await this.close();await this.open();}
  async tick(){
    if(!this.sessionId){await this.open();return;}
    const r=await cdp.send('Runtime.evaluate',{
      expression:'JSON.stringify({hook:!!window.__pdjCollectorHook,data:window.__pdjCollectorDrain?window.__pdjCollectorDrain():null,title:document.title,url:location.href})',
      returnByValue:true
    },this.sessionId);
    const raw=r?.result?.value;
    if(!raw)return;
    const result=JSON.parse(raw);
    this.title=String(result.title||'').slice(0,120);
    if(!result.hook && Date.now()-this.lastReload>30000)await this.reload();
    const data=result.data||{};
    if(data.lastIM)this.lastIM=data.lastIM;
    const frames=Array.isArray(data.frames)?data.frames:[];
    this.frames+=frames.length;
    for(const frame of frames){
      const controller=new AbortController();
      const timeout=setTimeout(()=>controller.abort(),8000);
      try{
        const reply=await fetch('http://127.0.0.1:'+port+'/api/douyin/bridge/frame',{
          method:'POST',
          headers:{'Content-Type':'application/json','X-PDJ-Bridge-Token':token},
          body:JSON.stringify({instance_id:this.instance,live_id:this.id,frame}),
          signal:controller.signal
        });
        if(reply.status===401){throw new Error('Go bridge token expired: restart collector');}
        if(reply.status===409){throw new Error('Douyin room/instance mismatch: check BiliPDJ-GO configuration');}
        if(!reply.ok)throw new Error('Go bridge returned HTTP '+reply.status);
        this.sent++;
      }catch(e){
        this.errors++;
        if(/token expired|room\/instance mismatch/.test(e.message))throw e;
      }finally{clearTimeout(timeout);}
    }
    // Do not repeatedly reload genuinely offline/quiet rooms without an IM baseline.
    if(this.lastIM && Date.now()-this.lastIM>150000 && Date.now()-this.lastReload>60000){
      await this.reload();
    }
  }
  snapshot(){
    return {live_id:this.id,instance_id:this.instance,title:this.title,frames:this.frames,
      sent:this.sent,errors:this.errors,last_im_at:this.lastIM?new Date(this.lastIM).toISOString():null,
      browser_page_open:!!this.sessionId};
  }
}
async function collector(){
  await bootChrome(true);
  cdp=await connectCDP();
  const authenticated=await getLoginStatus();
  report({login_state:authenticated?'cookie_detected':'login_required',
    logged_in:authenticated,checked_at:new Date().toISOString()});
  if(!authenticated)throw new Error('login expired: click Douyin Login in BiliPDJ-GO');
  const workers=rooms.map(r=>new Room(r.id,r.instance));
  for(const room of workers){try{await room.open()}catch(e){room.errors++;logError(e);}}
  while(!closing){
    if(await inspectStop())break;
    if(cdp.closed){
      logError(new Error('CDP disconnected; recovering Chrome session'));
      await closeBrowser();
      await sleep(2000);
      await bootChrome(true);
      cdp=await connectCDP();
      for(const room of workers){room.sessionId='';room.targetId='';}
    }
    for(const room of workers){
      try {await room.tick();}
      catch(e){room.errors++;logError(e);
        if(/token expired|room\/instance mismatch|login expired/.test(e.message))throw e;
        try{await room.reload()}catch(err){logError(err);room.sessionId='';}
      }
    }
    report({rooms:Object.fromEntries(workers.map(x=>[x.instance,x.snapshot()])),running:true});
    await sleep(1000);
  }
  await stop('requested');
}
async function main(){
  report();
  try{if(loginMode)await logIn();else await collector();}
  catch(e){logError(e);await stop('error');process.exitCode=1;}
}
main();

