/* Standalone queue console. Same REST/SSE and persisted slots as the main console. */
const {createApp,ref,computed,onMounted,onUnmounted}=Vue;
createApp({setup(){
 const queue=ref([]),slotInfo=ref({active_slot:1,slots:{}}),activeSlot=ref(1);
 const selectedKey=ref(''),search=ref(''),newName=ref('');
 const notice=ref(''),noticeLevel=ref('info'),busy=ref(false),streamReady=ref(false);
 const needsAuth=ref(false),tokenInput=ref(''),adminToken=ref('');
 const editing=ref(false),editNote=ref('');
 const visibleQueue=computed(()=>{
  const word=search.value.toLowerCase();
  return queue.value.filter(q=>!word||(String(q.username||'')+' '+String(q.note||'')+' '+String(q.mode||'')+' '+String(q.platform||'')+' '+String(q.user_id||'')).toLowerCase().includes(word));
 });
 const selectedIndex=computed(()=>queue.value.findIndex(q=>q.key===selectedKey.value));
 const selectedItem=computed(()=>selectedIndex.value>=0?queue.value[selectedIndex.value]:null);
 const slotCount=computed(()=>Number(slotInfo.value.slots?.[activeSlot.value]??queue.value.length));
 const platformName=p=>({bilibili:'B站',douyin:'抖音',huya:'虎牙',wechat_mp:'公众号',kuaishou:'快手',douyu:'斗鱼',manual:'手动'})[p]||p||'手动';
 const timeOf=t=>{if(!t)return '—';const d=new Date(t);return isNaN(d.getTime())?'—':d.toLocaleTimeString('zh-CN',{hour12:false})};
 const position=key=>queue.value.findIndex(q=>q.key===key)+1;
 let events=null,pollTimer=null,readSeq=0;
 function notify(msg,level='info'){notice.value=msg;noticeLevel.value=level;}
 function saveToken(){adminToken.value=tokenInput.value;tokenInput.value='';needsAuth.value=false;notify('授权信息已暂存当前页面；下一次修改时由服务器验证。');}
 async function api(path,options={}){
  const headers={'Content-Type':'application/json',...(options.headers||{})};
  if(adminToken.value)headers['X-Admin-Token']=adminToken.value;
  const res=await fetch(path,{...options,headers,cache:'no-store'});
  let data;
  try{data=await res.json()}catch{data=null}
  if(!res.ok){
   if(res.status===403){needsAuth.value=true;throw Error('修改被拒绝：请确认服务器管理员 Token；本机以外的设备必须授权。');}
   throw Error(data?.error||'服务器请求失败（HTTP '+res.status+'）');
  }
  return data;
 }
 function reconcileSelection(){if(selectedKey.value&&!queue.value.some(x=>x.key===selectedKey.value)){selectedKey.value='';editing.value=false;editNote.value='';}}
 async function refresh(){
  if(busy.value)return;
  const seq=++readSeq;
  try{
   const [state,slots]=await Promise.all([api('/api/queue/state'),api('/api/queue/slots')]);
   if(seq!==readSeq||busy.value)return;
   // Switching a slot in another tab can happen between the two GET requests.
   if(Number(state.active_slot)!==Number(slots.active_slot)){setTimeout(refresh,120);return;}
   queue.value=Array.isArray(state.entries)?state.entries:[];
   slotInfo.value=slots;
   activeSlot.value=Number(slots.active_slot)||1;
   reconcileSelection();
  }catch(e){if(seq===readSeq)notify('刷新失败：'+e.message,'error');}
 }
 async function loadAppearance(){
  try{
   const res=await fetch('/api/appearance',{cache:'no-store'});
   if(!res.ok)return;
   const appearance=await res.json(),mode=appearance.mode==='light'?'light':'dark';
   const colors=appearance[mode]||{},root=document.documentElement;
   const mapping={'--bg':'background','--panel':'surface','--panel2':'surface_alt','--line':'border','--text':'text','--dim':'muted','--accent':'accent'};
   for(const [css,key] of Object.entries(mapping)){
    if(typeof colors[key]==='string'&&/^#[0-9a-fA-F]{3,8}$/.test(colors[key]))root.style.setProperty(css,colors[key]);
   }
   root.style.colorScheme=mode;
  }catch{}
 }
 function selectItem(key){if(selectedKey.value===key){selectedKey.value='';editing.value=false;return;}
  selectedKey.value=key;editing.value=false;editNote.value='';
 }
 async function mutate(path,payload){
  if(busy.value)return false;
  busy.value=true;++readSeq;
  try{
   const data=await api(path,{method:'POST',body:JSON.stringify(payload)});
   if(path==='/api/queue/slots'){
    queue.value=Array.isArray(data.entries)?data.entries:[];
    slotInfo.value=data;activeSlot.value=data.active_slot||1;
    selectedKey.value='';editing.value=false;
   }else{
    queue.value=Array.isArray(data)?data:[];
    reconcileSelection();
   }
   notify('操作已保存并同步到当前队列。','success');
   return true;
  }catch(e){notify(e.message,'error');return false;}
  finally{busy.value=false;void refresh();}
 }
 async function switchSlot(event){
  const target=event.target,slot=Number(target.value);
  if(slot===activeSlot.value)return;
  const ok=await mutate('/api/queue/slots',{slot});
  if(!ok)target.value=String(activeSlot.value);
 }
 async function addQueue(){const name=newName.value.trim();if(!name)return;
  if(await mutate('/api/queue',{action:'add',name}))newName.value='';
 }
 async function insertQueue(){const name=newName.value.trim();if(!name)return;
  const index=selectedIndex.value<0?0:selectedIndex.value+1;
  if(await mutate('/api/queue',{action:'insert',name,index}))newName.value='';
 }
 async function moveSelected(delta){if(selectedIndex.value<0)return;
  const index=selectedIndex.value+delta;if(index<0||index>=queue.value.length)return;
  await mutate('/api/queue',{action:'move',key:selectedKey.value,index});
 }
 function startEditing(){if(!selectedItem.value)return;editNote.value=selectedItem.value.note||'';editing.value=true;}
 async function saveEdit(){if(!selectedItem.value)return;
  if(await mutate('/api/queue',{action:'edit',key:selectedItem.value.key,note:editNote.value}))editing.value=false;
 }
 async function removeSelected(){if(!selectedItem.value)return;
  const user=selectedItem.value;if(window.confirm('确认从当前槽位删除「'+user.username+'」？')){
   await mutate('/api/queue',{action:'remove',key:user.key});
  }
 }
 async function completeFirst(){if(!queue.value.length)return;const q=queue.value[0];
  if(window.confirm('确认完成并移除队首「'+q.username+'」？'))await mutate('/api/queue',{action:'remove',key:q.key});
 }
 async function clearQueue(){if(!queue.value.length)return;
  if(window.confirm('确认清空槽位 '+activeSlot.value+' 的全部 '+queue.value.length+' 人？此操作不可撤销。'))await mutate('/api/queue',{action:'clear'});
 }
 function connectSSE(){
  events=new EventSource('/api/events');
  events.onopen=()=>streamReady.value=true;
  events.onerror=()=>streamReady.value=false;
  events.onmessage=e=>{try{const msg=JSON.parse(e.data);if(msg.type==='queue')void refresh();}catch{}};
 }
 onMounted(()=>{void loadAppearance();void refresh();connectSSE();pollTimer=setInterval(refresh,8000);});
 onUnmounted(()=>{events?.close();clearInterval(pollTimer);});
 return {queue,slotInfo,activeSlot,selectedKey,selectedIndex,selectedItem,slotCount,visibleQueue,search,newName,notice,noticeLevel,
  busy,streamReady,needsAuth,tokenInput,saveToken,editing,editNote,position,platformName,timeOf,
  refresh,selectItem,switchSlot,addQueue,insertQueue,moveSelected,startEditing,saveEdit,removeSelected,completeFirst,clearQueue};
}}).mount('#app');
