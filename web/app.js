/* Vue 3 global build is bundled locally for fully-offline UI startup. */
const { createApp, ref, computed, onMounted, onUnmounted } = Vue;
createApp({setup(){
 const page=ref('dashboard'), status=ref({}), statuses=computed(()=>status.value.platforms||{});
 const config=ref({bilibili:{room:'',cookie:'',enabled:false},douyin:{room:'',cookie:'',enabled:false},auto_queue:true,command:'排队'});
 const legacyFile=ref(null), legacyPreview=ref(null), blacklistText=ref(''), adminsText=ref('');
 const cookieConfigured=ref({bilibili:false,douyin:false}), messages=ref([]),queue=ref([]),filter=ref('all');
 const notice=ref(''),noticeLevel=ref('info'),busy=ref(false),streamReady=ref(false),now=ref(''),newName=ref(''),release=ref(null);
 const platforms=[{id:'bilibili',name:'Bilibili 直播',placeholder:'直播间号码，如 6'},{id:'douyin',name:'抖音直播',placeholder:'live.douyin.com/xxxx'}];
 const filters=[{id:'all',name:'全部'},{id:'bilibili',name:'B站'},{id:'douyin',name:'抖音'}];
 const connectedCount=computed(()=>platforms.filter(p=>statuses.value[p.id]?.connected).length);
 const filteredMessages=computed(()=>[...messages.value].reverse().filter(m=>filter.value==='all'||filter.value===m.platform));
 const dateTime=t=>{try{return new Date(t).toLocaleTimeString('zh-CN',{hour12:false})}catch{return ''}};
 let eventStream=null, poller=null, clock=null;
 function message(t,level='info'){notice.value=t;noticeLevel.value=level}
 async function api(path,options={}){const headers={'Content-Type':'application/json',...(options.headers||{})};if(options.method&&options.method!=='GET'){
 const token=sessionStorage.getItem('pdj-token');if(token)headers['X-Admin-Token']=token;
 }
 const resp=await fetch(path,{...options,headers});let data={};try{data=await resp.json()}catch{}
 if(!resp.ok){if(resp.status===403){const token=window.prompt('管理接口需要管理员 Token（本机直接运行通常无需输入）：');if(token){sessionStorage.setItem('pdj-token',token);return api(path,options)}}throw Error(data.error||`HTTP ${resp.status}`)}return data;
 }
 async function refresh(){try{const [s,q,m]=await Promise.all([api('/api/status'),api('/api/queue'),api('/api/messages')]);status.value=s;queue.value=q;messages.value=m}catch(e){message(e.message,'error')}}
 async function loadAppearance(){try{const appearance=await api('/api/appearance');const mode=appearance.mode==='light'?'light':'dark';const colors=appearance[mode]||{};const root=document.documentElement;const mapping={'--bg':'background','--panel':'surface','--panel2':'surface_alt','--line':'border','--text':'text','--dim':'muted','--accent':'accent'};for(const [css,k] of Object.entries(mapping)){const value=colors[k];if(typeof value==='string' && /^#[0-9a-fA-F]{3,8}$/.test(value))root.style.setProperty(css,value)}root.style.colorScheme=mode}catch{}}
 async function loadConfig(){try{const d=await api('/api/config');config.value=d.config;blacklistText.value=(config.value.blacklist||[]).join('\n');adminsText.value=(config.value.admins||[]).join('\n');cookieConfigured.value=d.cookie_configured}catch(e){message(e.message,'error')}}
 async function saveConfig(){busy.value=true;try{config.value.blacklist=blacklistText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.admins=adminsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);await api('/api/config',{method:'POST',body:JSON.stringify(config.value)});message('已保存配置，正在重新连接平台','success');await Promise.all([refresh(),loadConfig()])}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function queueAction(body){try{queue.value=await api('/api/queue',{method:'POST',body:JSON.stringify(body)})}catch(e){message(e.message,'error')}}
 async function addQueue(){if(!newName.value)return;await queueAction({action:'add',name:newName.value});newName.value=''}
 function removeQueue(key){queueAction({action:'remove',key})}
 function clearQueue(){if(window.confirm('确认清空当前全部排队？'))queueAction({action:'clear'})}
 async function checkUpdate(){busy.value=true;try{release.value=await api('/api/update');message('更新检查已完成','success')}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function downloadUpdate(){busy.value=true;try{const r=await api('/api/update/download',{method:'POST',body:'{}'});message(`${r.message}：${r.file}`,'success')}catch(e){message(e.message,'error')}finally{busy.value=false}}

 function selectLegacyFile(e){legacyFile.value=e.target.files?.[0]||null;legacyPreview.value=null;}
 async function legacyAction(action){if(!legacyFile.value){message('请先选择旧版配置文件或备份 ZIP','error');return}
  if(action==='import'&&!window.confirm('确认导入并覆盖新版对应配置？旧版原始文件和当前新版配置都会先备份。'))return;
  busy.value=true;try{const form=new FormData();form.append('file',legacyFile.value);
   const headers={};const token=sessionStorage.getItem('pdj-token');if(token)headers['X-Admin-Token']=token;
   const response=await fetch('/api/legacy/'+action,{method:'POST',headers,body:form});const result=await response.json();if(!response.ok)throw Error(result.error||'导入失败');
   legacyPreview.value=result.preview||result;message(action==='preview'?'预览完成，不会修改配置':'导入成功：已备份当前配置和旧版原始文件','success');if(action==='import'){await Promise.all([loadConfig(),refresh(),loadAppearance()])}
  }catch(e){message(e.message,'error')}finally{busy.value=false}}
 function connectSSE(){eventStream=new EventSource('/api/events');eventStream.onopen=()=>{streamReady.value=true};eventStream.onerror=()=>{streamReady.value=false};eventStream.onmessage=e=>{try{const event=JSON.parse(e.data);if(event.type==='danmu'){messages.value.push(event.data);if(messages.value.length>150)messages.value.shift()}else if(event.type==='queue'){queue.value=event.data}else if(event.type==='status'){status.value.platforms={...(status.value.platforms||{}),[event.data.platform]:event.data}}}catch{}}}
 onMounted(()=>{refresh();loadConfig();loadAppearance();connectSSE();now.value=new Date().toLocaleTimeString('zh-CN',{hour12:false});clock=setInterval(()=>now.value=new Date().toLocaleTimeString('zh-CN',{hour12:false}),1000);poller=setInterval(refresh,20000)});
 onUnmounted(()=>{if(eventStream)eventStream.close();clearInterval(clock);clearInterval(poller)});
 return {page,status,statuses,config,cookieConfigured,messages,queue,filter,notice,noticeLevel,busy,streamReady,now,newName,release,platforms,filters,connectedCount,filteredMessages,dateTime,refresh,saveConfig,addQueue,removeQueue,clearQueue,checkUpdate,downloadUpdate,legacyFile,legacyPreview,blacklistText,adminsText,selectLegacyFile,legacyAction};
}}).mount('#app');
