/* Vue 3 global build is bundled locally for fully-offline UI startup. */
const { createApp, ref, computed, onMounted, onUnmounted } = Vue;
createApp({setup(){
 const page=ref('dashboard'), status=ref({}), statuses=computed(()=>status.value.platforms||{});
 const config=ref({bilibili:{room:'',cookie:'',enabled:false},douyin:{room:'',cookie:'',enabled:false},auto_queue:true,command:'排队',gift_queue:{enabled:false,names:[],min_batteries:0,allow_multiple:false,slots_per_gift:1,insert_rank:1,gift_only:false}});
 const obsStyle=ref({}), giftStatus=ref(null), giftNamesText=ref('');
 const qrImage=ref(''), qrState=ref(''), qrLink=ref('');let qrTimer=null, qrPolling=false;
 const legacyFile=ref(null), legacyPreview=ref(null), blacklistText=ref(''), adminsText=ref(''), superAdminsText=ref(''), guardsText=ref('');
 const cookieConfigured=ref({bilibili:false,douyin:false}), messages=ref([]),queue=ref([]),filter=ref('all'),slotInfo=ref({active_slot:1,slots:{}}),selectedSlot=ref(1);
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
 async function refresh(){try{const [s,q,m,slots]=await Promise.all([api('/api/status'),api('/api/queue'),api('/api/messages'),api('/api/queue/slots')]);status.value=s;queue.value=q;messages.value=m;slotInfo.value=slots;selectedSlot.value=slots.active_slot}catch(e){message(e.message,'error')}}
 async function loadAppearance(){try{const appearance=await api('/api/appearance');const mode=appearance.mode==='light'?'light':'dark';const colors=appearance[mode]||{};const root=document.documentElement;const mapping={'--bg':'background','--panel':'surface','--panel2':'surface_alt','--line':'border','--text':'text','--dim':'muted','--accent':'accent'};for(const [css,k] of Object.entries(mapping)){const value=colors[k];if(typeof value==='string' && /^#[0-9a-fA-F]{3,8}$/.test(value))root.style.setProperty(css,value)}root.style.colorScheme=mode}catch{}}
 async function loadConfig(){try{const d=await api('/api/config');config.value=d.config;if(!config.value.gift_queue)config.value.gift_queue={enabled:false,names:[],min_batteries:0,allow_multiple:false,slots_per_gift:1,insert_rank:1,gift_only:false};giftNamesText.value=(config.value.gift_queue.names||[]).join('\n');if(!config.value.switches)config.value.switches={paidui:true,guanfu_paidui:true,bfu_paidui:true,chaoji_paidui:true,mifu_paidui:true,quxiao_paidui:true,xiugai_paidui:true,jianzhang_chadui:false,fangguan_op:false};selectedSlot.value=config.value.archive_slot||1;blacklistText.value=(config.value.blacklist||[]).join('\n');adminsText.value=(config.value.admins||[]).join('\n');superAdminsText.value=(config.value.super_admins||[]).join('\n');guardsText.value=(config.value.guards||[]).join('\n');cookieConfigured.value=d.cookie_configured}catch(e){message(e.message,'error')}}
 async function saveConfig(){busy.value=true;try{config.value.gift_queue.names=giftNamesText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.blacklist=blacklistText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.admins=adminsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.super_admins=superAdminsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.guards=guardsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);await api('/api/config',{method:'POST',body:JSON.stringify(config.value)});message('已保存配置，正在重新连接平台','success');await Promise.all([refresh(),loadConfig()])}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function loadGiftStatus(){try{giftStatus.value=await api('/api/gifts/state')}catch(e){message(e.message,'error')}}
 async function loadObsStyle(){try{obsStyle.value=await api('/api/style')}catch(e){message(e.message,'error')}}
 async function saveObsStyle(){busy.value=true;try{await api('/api/style',{method:'POST',body:JSON.stringify(obsStyle.value)});message('OBS 样式已保存','success')}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function queueAction(body){try{queue.value=await api('/api/queue',{method:'POST',body:JSON.stringify(body)})}catch(e){message(e.message,'error')}}
 async function changeSlot(){try{const result=await api('/api/queue/slots',{method:'POST',body:JSON.stringify({slot:Number(selectedSlot.value)})});queue.value=result.entries;slotInfo.value=result;config.value.archive_slot=result.active_slot;message('已切换到存档 '+result.active_slot,'success')}catch(e){message(e.message,'error');selectedSlot.value=slotInfo.value.active_slot}}
 async function moveQueue(q,index){await queueAction({action:'move',key:q.key,index:index})}
 async function editQueue(q){const note=window.prompt('修改 '+q.username+' 的备注',q.note||'');if(note!==null)await queueAction({action:'edit',key:q.key,note})}
 async function addQueue(){if(!newName.value)return;await queueAction({action:'add',name:newName.value});newName.value=''}
 function removeQueue(key){queueAction({action:'remove',key})}
 function clearQueue(){if(window.confirm('确认清空当前全部排队？'))queueAction({action:'clear'})}
 async function checkUpdate(){busy.value=true;try{release.value=await api('/api/update');message('更新检查已完成','success')}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function downloadUpdate(){busy.value=true;try{const r=await api('/api/update/download',{method:'POST',body:'{}'});message(`${r.message}：${r.file}`,'success')}catch(e){message(e.message,'error')}finally{busy.value=false}}

 function qrSVG(raw){
  if(!window.PDJQR)throw Error('二维码组件未加载');
  const bits=window.PDJQR(raw),n=bits.length,pad=4;let rects='';
  for(let y=0;y<n;y++)for(let x=0;x<n;x++)if(bits[y][x])rects+=`<rect x="${x+pad}" y="${y+pad}" width="1" height="1"/>`;
  const svg=`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n+pad*2} ${n+pad*2}" width="320" height="320"><rect width="100%" height="100%" fill="white"/><g fill="black">${rects}</g></svg>`;
  return 'data:image/svg+xml;charset=utf-8,'+encodeURIComponent(svg);
 }
 function stopQR(){if(qrTimer!==null){clearInterval(qrTimer);qrTimer=null}}
 async function pollQR(){if(qrPolling || !qrImage.value)return;qrPolling=true;
  try{const result=await api('/api/bili/qr/poll',{method:'POST',body:'{}'});
   if(result.status==='success'){stopQR();qrImage.value='';qrLink.value='';qrState.value='扫码登录成功：'+(result.username||result.uid);message(qrState.value,'success');await loadConfig();}
   else{qrState.value=result.message||'等待手机扫码';}
  }catch(e){stopQR();qrImage.value='';qrState.value=e.message;message('B站扫码：'+e.message,'error')}
  finally{qrPolling=false}
 }
 async function startQR(){stopQR();qrImage.value='';qrLink.value='';qrState.value='正在生成二维码…';try{
  const r=await api('/api/bili/qr/start',{method:'POST',body:'{}'});
  qrImage.value=qrSVG(r.url);qrLink.value=r.url;qrState.value='使用哔哩哔哩手机 App 扫码，并在手机端确认登录';qrTimer=setInterval(pollQR,2500);
 }catch(e){qrState.value=e.message;message(e.message,'error')}}
 async function logoutBili(){if(!window.confirm('确认清除 Go 版已保存的 B站登录 Cookie？'))return;
  stopQR();qrImage.value='';try{await api('/api/bili/logout',{method:'POST',body:'{}'});qrState.value='已清除登录会话';await loadConfig();message('已清除 B站登录 Cookie','success')}catch(e){message(e.message,'error')}
 }
 function selectLegacyFile(e){legacyFile.value=e.target.files?.[0]||null;legacyPreview.value=null;}
 async function legacyAction(action){if(!legacyFile.value){message('请先选择旧版配置文件或备份 ZIP','error');return}
  if(action==='import'&&!window.confirm('确认导入并覆盖新版对应配置？旧版原始文件和当前新版配置都会先备份。'))return;
  busy.value=true;try{const form=new FormData();form.append('file',legacyFile.value);
   const headers={};const token=sessionStorage.getItem('pdj-token');if(token)headers['X-Admin-Token']=token;
   const response=await fetch('/api/legacy/'+action,{method:'POST',headers,body:form});const result=await response.json();if(!response.ok)throw Error(result.error||'导入失败');
   legacyPreview.value=result.preview||result;message(action==='preview'?'预览完成，不会修改配置':'导入成功：已备份当前配置和旧版原始文件','success');if(action==='import'){await Promise.all([loadConfig(),refresh(),loadAppearance()])}
  }catch(e){message(e.message,'error')}finally{busy.value=false}}
 function connectSSE(){eventStream=new EventSource('/api/events');eventStream.onopen=()=>{streamReady.value=true};eventStream.onerror=()=>{streamReady.value=false};eventStream.onmessage=e=>{try{const event=JSON.parse(e.data);if(event.type==='danmu'){messages.value.push(event.data);if(messages.value.length>150)messages.value.shift()}else if(event.type==='queue'){queue.value=event.data}else if(event.type==='status'){status.value.platforms={...(status.value.platforms||{}),[event.data.platform]:event.data}}}catch{}}}
 onMounted(()=>{refresh();loadConfig();loadAppearance();loadObsStyle();connectSSE();now.value=new Date().toLocaleTimeString('zh-CN',{hour12:false});clock=setInterval(()=>now.value=new Date().toLocaleTimeString('zh-CN',{hour12:false}),1000);poller=setInterval(refresh,20000)});
 onUnmounted(()=>{if(eventStream)eventStream.close();clearInterval(clock);clearInterval(poller);stopQR()});
 return {page,status,statuses,config,cookieConfigured,messages,queue,filter,notice,noticeLevel,busy,streamReady,now,newName,release,platforms,filters,connectedCount,filteredMessages,dateTime,refresh,saveConfig,addQueue,removeQueue,clearQueue,moveQueue,editQueue,changeSlot,slotInfo,selectedSlot,checkUpdate,downloadUpdate,giftStatus,giftNamesText,loadGiftStatus,obsStyle,saveObsStyle,legacyFile,legacyPreview,blacklistText,adminsText,superAdminsText,guardsText,selectLegacyFile,legacyAction,qrImage,qrLink,qrState,startQR,logoutBili};
}}).mount('#app');
