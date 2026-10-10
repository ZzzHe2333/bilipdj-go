/* Vue 3 global build is bundled locally for fully-offline UI startup. */
const { createApp, ref, computed, onMounted, onUnmounted, nextTick, watch } = Vue;
createApp({setup(){
 const page=ref('logs'), status=ref({}), statuses=computed(()=>status.value.platforms||{});
 const wizardOpen=ref(false),wizardStep=ref(0),wizardBusy=ref(false),wizardLoaded=ref(false),wizardError=ref('');
 const config=ref({bilibili:{room:'',cookie:'',enabled:false},douyin:{room:'',cookie:'',enabled:false},auto_queue:true,command:'排队',gift_queue:{enabled:false,names:[],min_batteries:0,allow_multiple:false,slots_per_gift:1,insert_rank:1,gift_only:false}});
 const obsStyle=ref({}), giftStatus=ref(null), giftNamesText=ref('');
 const qrImage=ref(''), qrState=ref(''), qrLink=ref('');let qrTimer=null, qrPolling=false;
 const legacyFile=ref(null), legacyPreview=ref(null), blacklistText=ref(''), adminsText=ref(''), superAdminsText=ref(''), guardsText=ref('');
 const cookieConfigured=ref({bilibili:false,douyin:false}), messages=ref([]),queue=ref([]),filter=ref('all'),slotInfo=ref({active_slot:1,slots:{}}),selectedSlot=ref(1);
 const storageInfo=ref({}),pythonSlots=ref({counts:{}}),storageDecisionDismissed=ref(false),storageBusy=ref(false);
 const logs=ref([]),logLevel=ref('ALL'),logCategory=ref('all'),logSearch=ref(''),autoScroll=ref(true),logListRef=ref(null),queueSearch=ref(''),selectedKey=ref('');
 const notice=ref(''),noticeLevel=ref('info'),busy=ref(false),streamReady=ref(false),now=ref(''),newName=ref(''),release=ref(null),updateSource=ref('auto'),updateReady=ref(null);
 const platforms=[{id:'bilibili',name:'Bilibili 直播',placeholder:'直播间号码，如 6'},{id:'douyin',name:'抖音直播',placeholder:'live.douyin.com/xxxx'}];
 const platformCatalog=[
  {id:'bilibili',name:'Bilibili 直播',supported:true,placeholder:'B站直播间号'},
  {id:'douyin',name:'抖音直播',supported:true,placeholder:'直播间号或 live.douyin.com 链接'},
  {id:'huya',name:'虎牙直播',supported:false,placeholder:'虎牙房间号',description:'虎牙弹幕监听暂未接入，只保存配置。'},
  {id:'wechat_mp',name:'微信公众号',supported:false,placeholder:'公众号标识（预留）',description:'微信公众号目前只有配置占位。'},
  {id:'kuaishou',name:'快手直播',supported:false,placeholder:'快手房间号（预留）',description:'快手直播弹幕监听暂未接入。'},
  {id:'douyu',name:'斗鱼直播',supported:false,placeholder:'斗鱼房间号（预留）',description:'斗鱼直播弹幕监听暂未接入。'}
 ];
 const showPlatformPicker=ref(false),previousBiliEnabled=ref(false);
 const configuredPlatforms=computed(()=>platformCatalog.filter(p=>(config.value.visible_platforms||[]).includes(p.id)));
 const addablePlatforms=computed(()=>platformCatalog.filter(p=>!(config.value.visible_platforms||[]).includes(p.id)));
 const monitorPlatforms=computed(()=>configuredPlatforms.value.filter(p=>p.supported&&config.value[p.id]?.enabled));
 const enabledMonitorCount=computed(()=>monitorPlatforms.value.length);
 function addPlatform(id){if(!platformCatalog.some(p=>p.id===id))return;
  if(!config.value[id])config.value[id]={enabled:false,room:'',cookie:''};
  if(!(config.value.visible_platforms||[]).includes(id))config.value.visible_platforms=[...(config.value.visible_platforms||[]),id];
  showPlatformPicker.value=false;
 }
 function removePlatform(id){if(config.value[id])config.value[id].enabled=false;
  config.value.visible_platforms=(config.value.visible_platforms||[]).filter(x=>x!==id);
  if(id==='bilibili')stopQR();message('已移除平台；点击保存后停止监听','info');
 }
 function normalizeDouyinRoom(raw){const value=String(raw||'').trim();if(!value)return '';
  if(/^[0-9A-Za-z_-]{1,100}$/.test(value))return value;
  const hit=value.match(/https?:\/\/live\.douyin\.com\/[0-9A-Za-z_-]{1,100}(?:[/?#][^\s，。]*)?/i);
  if(!hit)throw Error('请输入直播间号或 live.douyin.com 链接；短链接需先在浏览器展开');
  const url=new URL(hit[0].replace(/[;；、)）]+$/g,''));
  if(url.hostname!=='live.douyin.com'||url.port||url.username||url.password)throw Error('抖音链接不合法');
  const id=url.pathname.split('/').filter(Boolean)[0]||'';
  if(!/^[0-9A-Za-z_-]{1,100}$/.test(id))throw Error('无法解析直播间号');
  return id;
 }
 function normalizeDouyinField(){try{if(config.value.douyin?.room)config.value.douyin.room=normalizeDouyinRoom(config.value.douyin.room)}catch(e){message(e.message,'error')}}

 const filters=[{id:'all',name:'全部'},{id:'bilibili',name:'B站'},{id:'douyin',name:'抖音'}];
 const logCategoryName=category=>({system:'系统',queue:'排队',bilibili:'B站',douyin:'抖音'})[category]||category;
 const platformName=platform=>({bilibili:'B站',douyin:'抖音',huya:'虎牙',wechat_mp:'微信公众号',kuaishou:'快手',douyu:'斗鱼'})[platform]||platform||'无来源';
 const queueSourceLabel=q=>q.platform==='manual'?platformName(q.source_platform):platformName(q.platform);
 const logOrder={DEBUG:0,INFO:1,WARNING:2,ERROR:3,CRITICAL:4};
 const filteredLogs=computed(()=>logs.value.filter(l=>(logLevel.value==='ALL'||logOrder[l.level]>=logOrder[logLevel.value])&&(logCategory.value==='all'||l.category===logCategory.value)&&(!logSearch.value||`${l.level} ${l.category} ${l.message}`.toLowerCase().includes(logSearch.value.toLowerCase()))));
 const visibleQueue=computed(()=>queue.value.filter(q=>!queueSearch.value||`${q.username} ${q.note} ${q.mode} ${q.platform} ${q.source_platform||''} ${q.user_id}`.toLowerCase().includes(queueSearch.value.toLowerCase())));
 const selectedIndex=computed(()=>queue.value.findIndex(q=>q.key===selectedKey.value));
 const connectedCount=computed(()=>monitorPlatforms.value.filter(p=>statuses.value[p.id]?.connected).length);
 const filteredMessages=computed(()=>[...messages.value].reverse().filter(m=>filter.value==='all'||filter.value===m.platform));
 const dateTime=t=>{try{return new Date(t).toLocaleTimeString('zh-CN',{hour12:false})}catch{return ''}};
 let eventStream=null, poller=null, clock=null;
 function message(t,level='info'){notice.value=t;noticeLevel.value=level}
 async function api(path,options={}){const headers={'Content-Type':'application/json',...(options.headers||{})};
 const token=sessionStorage.getItem('pdj-token');if(token)headers['X-Admin-Token']=token;
 const resp=await fetch(path,{...options,headers});let data={};try{data=await resp.json()}catch{}
 if(!resp.ok){if(resp.status===403){const token=window.prompt('管理接口需要管理员 Token（本机直接运行通常无需输入）：');if(token){sessionStorage.setItem('pdj-token',token);return api(path,options)}}throw Error(data.error||`HTTP ${resp.status}`)}return data;
 }
 function appendLog(entry){if(!entry||!entry.id)return;if(logs.value.some(l=>l.id===entry.id))return;logs.value.push(entry);logs.value.sort((a,b)=>a.id-b.id);if(logs.value.length>500)logs.value.splice(0,logs.value.length-500)}
 async function refreshLogs(){try{const data=await api('/api/logs');for(const e of data)appendLog(e)}catch(e){message('读取日志失败：'+e.message,'error')}}
 function formatLog(l){return `${new Date(l.time).toLocaleString('zh-CN',{hour12:false})} [${l.level}] [${logCategoryName(l.category)}] ${l.message}`}
 async function copyLogs(){const txt=filteredLogs.value.map(formatLog).join('\n');try{await navigator.clipboard.writeText(txt);message('已复制 '+filteredLogs.value.length+' 条日志','success')}catch(e){message('复制失败，请使用导出 TXT：'+e.message,'error')}}
 function exportLogs(){const content=filteredLogs.value.map(formatLog).join('\n')+'\n';const blob=new Blob([content],{type:'text/plain;charset=utf-8'});const link=URL.createObjectURL(blob);const a=document.createElement('a');a.href=link;a.download='bilipdj-log-'+new Date().toISOString().slice(0,10)+'.txt';a.click();URL.revokeObjectURL(link)}
 function clearLogView(){logs.value=[];message('已清空浏览器当前显示的日志；不会删除服务端日志','info')}
 watch([filteredLogs,autoScroll],()=>{if(autoScroll.value)nextTick(()=>{const el=logListRef.value;if(el)el.scrollTop=el.scrollHeight})},{flush:'post'});
 async function refresh(){try{const [s,q,m,slots]=await Promise.all([api('/api/status'),api('/api/queue'),api('/api/messages'),api('/api/queue/slots')]);status.value=s;queue.value=q;messages.value=m;slotInfo.value=slots;selectedSlot.value=slots.active_slot}catch(e){message(e.message,'error')}}
 async function loadAppearance(){try{const appearance=await api('/api/appearance');const mode=appearance.mode==='light'?'light':'dark';const colors=appearance[mode]||{};const root=document.documentElement;const mapping={'--bg':'background','--panel':'surface','--panel2':'surface_alt','--line':'border','--text':'text','--dim':'muted','--accent':'accent'};for(const [css,k] of Object.entries(mapping)){const value=colors[k];if(typeof value==='string' && /^#[0-9a-fA-F]{3,8}$/.test(value))root.style.setProperty(css,value)}root.style.colorScheme=mode}catch{}}
 async function loadConfig(){try{const d=await api('/api/config');config.value=d.config;previousBiliEnabled.value=!!config.value.bilibili?.enabled;if(!Array.isArray(config.value.visible_platforms))config.value.visible_platforms=platformCatalog.filter(p=>config.value[p.id]?.enabled).map(p=>p.id);if(!config.value.gift_queue)config.value.gift_queue={enabled:false,names:[],min_batteries:0,allow_multiple:false,slots_per_gift:1,insert_rank:1,gift_only:false};giftNamesText.value=(config.value.gift_queue.names||[]).join('\n');if(!config.value.switches)config.value.switches={paidui:true,guanfu_paidui:true,bfu_paidui:true,chaoji_paidui:true,mifu_paidui:true,quxiao_paidui:true,xiugai_paidui:true,jianzhang_chadui:false,fangguan_op:false};selectedSlot.value=config.value.archive_slot||1;blacklistText.value=(config.value.blacklist||[]).join('\n');adminsText.value=(config.value.admins||[]).join('\n');superAdminsText.value=(config.value.super_admins||[]).join('\n');guardsText.value=(config.value.guards||[]).join('\n');cookieConfigured.value=d.cookie_configured}catch(e){message(e.message,'error')}}
 async function saveConfig(){busy.value=true;try{if(config.value.douyin?.room)config.value.douyin.room=normalizeDouyinRoom(config.value.douyin.room);if(config.value.bilibili?.enabled&&!cookieConfigured.value.bilibili&&!String(config.value.bilibili.cookie||'').trim()&&!previousBiliEnabled.value)throw Error('请先扫码登录 B站，再开启弹幕监听');config.value.gift_queue.names=giftNamesText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.blacklist=blacklistText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.admins=adminsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.super_admins=superAdminsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);config.value.guards=guardsText.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean);await api('/api/config',{method:'POST',body:JSON.stringify(config.value)});message('已保存配置，正在重新连接平台','success');await Promise.all([refresh(),loadConfig()]);return true}catch(e){message(e.message,'error');return false}finally{busy.value=false}}
 async function loadGiftStatus(){try{giftStatus.value=await api('/api/gifts/state')}catch(e){message(e.message,'error')}}
 async function loadObsStyle(){try{obsStyle.value=await api('/api/style')}catch(e){message(e.message,'error')}}
 async function saveObsStyle(){busy.value=true;try{await api('/api/style',{method:'POST',body:JSON.stringify(obsStyle.value)});message('OBS 样式已保存','success')}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function queueAction(body){try{queue.value=await api('/api/queue',{method:'POST',body:JSON.stringify(body)});if(selectedKey.value&&!queue.value.some(q=>q.key===selectedKey.value))selectedKey.value=''}catch(e){message(e.message,'error')}}
 async function changeSlot(){try{const result=await api('/api/queue/slots',{method:'POST',body:JSON.stringify({slot:Number(selectedSlot.value)})});queue.value=result.entries;selectedKey.value='';slotInfo.value=result;config.value.archive_slot=result.active_slot;message('已切换到存档 '+result.active_slot,'success')}catch(e){message(e.message,'error');selectedSlot.value=slotInfo.value.active_slot}}
 async function insertQueue(){if(!newName.value)return;const name=newName.value;const index=selectedIndex.value<0?0:selectedIndex.value+1;await queueAction({action:'insert',name,index});newName.value=''}
 async function moveSelected(delta){if(selectedIndex.value<0)return;await moveQueue(queue.value[selectedIndex.value],selectedIndex.value+delta)}
 async function editSelected(){if(selectedIndex.value<0)return;await editQueue(queue.value[selectedIndex.value])}
 async function removeSelected(){if(selectedIndex.value<0)return;const q=queue.value[selectedIndex.value];if(window.confirm('确认移除 '+q.username+'？'))await removeQueue(q.key)}
 async function completeFirst(){if(!queue.value.length)return;if(window.confirm('确认完成并移除队首 '+queue.value[0].username+'？'))await queueAction({action:'remove',key:queue.value[0].key})}
 async function moveQueue(q,index){await queueAction({action:'move',key:q.key,index:index})}
 async function editQueue(q){const note=window.prompt('修改 '+q.username+' 的备注',q.note||'');if(note!==null)await queueAction({action:'edit',key:q.key,note})}
 async function addQueue(){if(!newName.value)return;await queueAction({action:'add',name:newName.value});newName.value=''}
 function removeQueue(key){queueAction({action:'remove',key})}
 function clearQueue(){if(window.confirm('确认清空当前全部排队？'))queueAction({action:'clear'})}
 async function checkUpdate(){busy.value=true;updateReady.value=null;try{release.value=await api('/api/update?source='+encodeURIComponent(updateSource.value));message('更新检查完成（'+(release.value.source==='accelerated'?'加速线路':'官方线路')+'）','success')}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function downloadUpdate(){busy.value=true;try{const r=await api('/api/update/download',{method:'POST',body:JSON.stringify({source:updateSource.value})});updateReady.value=r;message('已下载且 SHA-256 校验通过，可安装并重启','success')}catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function installUpdate(){if(!updateReady.value)return;
  if(!window.confirm('确认安装 '+updateReady.value.version+' 并重启 BiliPDJ Go？\n安装将短暂中断直播监听。旧版程序会保留备份。'))return;
  busy.value=true;try{const r=await api('/api/update/install',{method:'POST',body:'{}'});message(r.message+'。请等待浏览器自动重新打开；如果失败可从托盘打开或手动启动。','success');updateReady.value=null}
  catch(e){message('安装未启动：'+e.message,'error')}finally{busy.value=false}}


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
   if(result.status==='success'){stopQR();qrImage.value='';qrLink.value='';qrState.value='扫码登录成功：'+(result.username||result.uid);message(qrState.value,'success');const d=await api('/api/config');cookieConfigured.value=d.cookie_configured;}
   else{qrState.value=result.message||'等待手机扫码';}
  }catch(e){stopQR();qrImage.value='';qrState.value=e.message;message('B站扫码：'+e.message,'error')}
  finally{qrPolling=false}
 }
 async function startQR(){stopQR();qrImage.value='';qrLink.value='';qrState.value='正在生成二维码…';try{
  const r=await api('/api/bili/qr/start',{method:'POST',body:'{}'});
  qrImage.value=qrSVG(r.url);qrLink.value=r.url;qrState.value='使用哔哩哔哩手机 App 扫码，并在手机端确认登录';qrTimer=setInterval(pollQR,2500);
 }catch(e){qrState.value=e.message;message(e.message,'error')}}
 async function logoutBili(){if(!window.confirm('确认清除 Go 版已保存的 B站登录 Cookie？'))return;
  stopQR();qrImage.value='';try{await api('/api/bili/logout',{method:'POST',body:'{}'});qrState.value='已清除登录会话';const d=await api('/api/config');cookieConfigured.value=d.cookie_configured;message('已清除 B站登录 Cookie','success')}catch(e){message(e.message,'error')}
 }
 async function checkOnboarding(){try{const response=await api('/api/onboarding');wizardLoaded.value=true;if(!response.completed){wizardStep.value=0;wizardError.value='';if(config.value.bilibili?.room==='3049445'&&!config.value.bilibili.enabled)config.value.bilibili.room='';wizardOpen.value=true}}catch(e){message('读取首次配置向导状态失败：'+e.message,'error')}}
 function openWizard(){wizardStep.value=0;wizardOpen.value=true;wizardError.value='';wizardLoaded.value=true}
 async function closeWizard(){if(wizardBusy.value)return;wizardBusy.value=true;try{
   await api('/api/onboarding',{method:'POST',body:JSON.stringify({completed:true})});
   wizardOpen.value=false;stopQR();qrImage.value='';
   message('已跳过新手引导；可从左侧菜单随时重新打开','info');
 }catch(e){wizardError.value='保存跳过状态失败：'+e.message;message(wizardError.value,'error')}finally{wizardBusy.value=false}}
 async function finishWizard(){if(wizardBusy.value)return;
   if(config.value.bilibili.enabled&&!String(config.value.bilibili.room||'').trim()){wizardError.value='请填写 B站直播间号，或暂时关闭 B站监听';wizardStep.value=1;return}
   if(config.value.douyin.enabled&&!String(config.value.douyin.room||'').trim()){wizardError.value='请填写抖音直播间地址，或暂时关闭抖音监听';wizardStep.value=1;return}
   wizardBusy.value=true;
   if(config.value.bilibili.enabled)config.value.visible_platforms=[...new Set([...(config.value.visible_platforms||[]),'bilibili'])];
   if(config.value.douyin.enabled)config.value.visible_platforms=[...new Set([...(config.value.visible_platforms||[]),'douyin'])];
   try{const saved=await saveConfig();if(!saved){wizardError.value='保存失败，请检查页面顶部提示或配置内容';return;}
      await api('/api/onboarding',{method:'POST',body:JSON.stringify({completed:true})});
      wizardOpen.value=false;stopQR();qrImage.value='';message('初始设置完成，平台监听已更新','success');
   }catch(e){wizardError.value='完成引导失败：'+e.message;message(wizardError.value,'error')}finally{wizardBusy.value=false}
 }
 function selectLegacyFile(e){legacyFile.value=e.target.files?.[0]||null;legacyPreview.value=null;}
 async function legacyAction(action){if(!legacyFile.value){message('请先选择旧版配置文件或备份 ZIP','error');return}
  if(action==='import'&&!window.confirm('确认导入并覆盖新版对应配置？旧版原始文件和当前新版配置都会先备份。'))return;
  busy.value=true;try{const form=new FormData();form.append('file',legacyFile.value);
   const headers={};const token=sessionStorage.getItem('pdj-token');if(token)headers['X-Admin-Token']=token;
   const response=await fetch('/api/legacy/'+action,{method:'POST',headers,body:form});const result=await response.json();if(!response.ok)throw Error(result.error||'导入失败');
   legacyPreview.value=result.preview||result;message(action==='preview'?'预览完成，不会修改配置':'导入成功：已备份当前配置和旧版原始文件','success');if(action==='import'){await Promise.all([loadConfig(),refresh(),loadAppearance()])}
  }catch(e){message(e.message,'error')}finally{busy.value=false}}
 async function loadStorage(){try{storageInfo.value=await api('/api/storage/status');await loadPythonSlots()}catch(e){message('读取存储状态失败：'+e.message,'error')}}
 async function loadPythonSlots(){try{pythonSlots.value=await api('/api/storage/python-queues')}catch(e){pythonSlots.value={counts:{}}}}
 async function chooseStorage(choice){storageBusy.value=true;try{const result=await api('/api/storage/choice',{method:'POST',body:JSON.stringify({choice})});storageInfo.value={...storageInfo.value,choice:result.choice,conflict:false,restart_required:false};storageDecisionDismissed.value=true;message('已确认继续使用 bilipdj-go 独立目录；旧 Go 和 Python 数据未被覆盖。','success')}catch(e){message('保存存储选择失败：'+e.message,'error')}finally{storageBusy.value=false}}
 async function importPythonQueues(){if(!window.confirm('将 Python archives 目录中的排队 CSV 导入 Go 的对应槽位？Go 原存档将先备份，Python CSV 保持不变。'))return;storageBusy.value=true;try{const v=await api('/api/storage/python-queues/import',{method:'POST',body:'{}'});message(v.message||'存档已导入','success');await refresh()}catch(e){message('导入失败：'+e.message,'error')}finally{storageBusy.value=false}}
 async function exportPythonQueue(){try{const headers={};const token=sessionStorage.getItem('pdj-token');if(token)headers['X-Admin-Token']=token;const resp=await fetch('/api/storage/queue-export?slot='+Number(selectedSlot.value),{headers});if(!resp.ok)throw Error('HTTP '+resp.status);const blob=await resp.blob();const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download='queue_archive_slot_'+selectedSlot.value+'.csv';a.click();URL.revokeObjectURL(url)}catch(e){message('导出失败：'+e.message,'error')}}
 function connectSSE(){eventStream=new EventSource('/api/events');eventStream.onopen=()=>{streamReady.value=true};eventStream.onerror=()=>{streamReady.value=false};eventStream.onmessage=e=>{try{const event=JSON.parse(e.data);if(event.type==='danmu'){messages.value.push(event.data);if(messages.value.length>150)messages.value.shift()}else if(event.type==='queue'){queue.value=event.data}else if(event.type==='log'){appendLog(event.data)}else if(event.type==='status'){status.value.platforms={...(status.value.platforms||{}),[event.data.platform]:event.data}}}catch{}}}
 onMounted(()=>{refresh();refreshLogs();loadConfig().then(checkOnboarding);loadAppearance();loadObsStyle();loadStorage();connectSSE();now.value=new Date().toLocaleTimeString('zh-CN',{hour12:false});clock=setInterval(()=>now.value=new Date().toLocaleTimeString('zh-CN',{hour12:false}),1000);poller=setInterval(refresh,20000)});
 onUnmounted(()=>{if(eventStream)eventStream.close();clearInterval(clock);clearInterval(poller);stopQR()});
 return {platformCatalog,configuredPlatforms,addablePlatforms,monitorPlatforms,enabledMonitorCount,showPlatformPicker,addPlatform,removePlatform,normalizeDouyinField,page,storageInfo,pythonSlots,storageDecisionDismissed,storageBusy,loadStorage,loadPythonSlots,chooseStorage,importPythonQueues,exportPythonQueue,wizardOpen,wizardStep,wizardBusy,wizardLoaded,wizardError,openWizard,closeWizard,finishWizard,logs,logLevel,logCategory,logSearch,autoScroll,logListRef,filteredLogs,logCategoryName,refreshLogs,copyLogs,exportLogs,clearLogView,queueSearch,selectedKey,selectedIndex,visibleQueue,platformName,queueSourceLabel,insertQueue,moveSelected,editSelected,removeSelected,completeFirst,status,statuses,config,cookieConfigured,messages,queue,filter,notice,noticeLevel,busy,streamReady,now,newName,release,platforms,filters,connectedCount,filteredMessages,dateTime,refresh,saveConfig,addQueue,removeQueue,clearQueue,moveQueue,editQueue,changeSlot,slotInfo,selectedSlot,checkUpdate,downloadUpdate,installUpdate,updateSource,updateReady,giftStatus,giftNamesText,loadGiftStatus,obsStyle,saveObsStyle,legacyFile,legacyPreview,blacklistText,adminsText,superAdminsText,guardsText,selectLegacyFile,legacyAction,qrImage,qrLink,qrState,startQR,logoutBili};
}}).mount('#app');
