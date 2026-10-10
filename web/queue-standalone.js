/* Standalone queue console. Same REST/SSE and persisted slots as the main console. */
const {createApp,ref,computed,onMounted,onUnmounted,nextTick}=Vue;
createApp({setup(){
 const queue=ref([]),slotInfo=ref({active_slot:1,slots:{}}),activeSlot=ref(1);
 const selectedKey=ref(''),search=ref(''),newName=ref('');
 const sorting=ref(false),sortBefore=ref([]),sortKeys=ref([]),dragging=ref('');
 const sortedQueue=computed(()=>sorting.value?sortKeys.value.map(k=>queue.value.find(q=>q.key===k)).filter(Boolean):queue.value);
 function beginSorting(){if(busy.value)return;search.value='';sortBefore.value=queue.value.map(q=>q.key);sortKeys.value=[...sortBefore.value];sorting.value=true}
 function cancelSorting(){sorting.value=false;sortBefore.value=[];sortKeys.value=[];dragging.value=''}
 function dragStart(event,key){if(!sorting.value)return;dragging.value=key;event.dataTransfer.effectAllowed='move';event.dataTransfer.setData('text/plain',key)}
 function dragOver(event){if(sorting.value)event.preventDefault()}
 function dragDrop(event,target){if(!sorting.value)return;event.preventDefault();const a=sortKeys.value.indexOf(dragging.value),b=sortKeys.value.indexOf(target);if(a<0||b<0||a===b)return;const next=[...sortKeys.value];next.splice(b,0,next.splice(a,1)[0]);sortKeys.value=next}
 async function saveSorting(){
  if(!sorting.value||busy.value)return;busy.value=true;++readSeq;
  try{queue.value=await api('/api/queue',{method:'POST',body:JSON.stringify({action:'reorder',keys:sortKeys.value,before:sortBefore.value})});
   cancelSorting();notify('排队排序已保存并同步','success')
  }catch(e){notify('排序未保存：'+e.message,'error');cancelSorting()}
  finally{busy.value=false;void refresh()}
 }
 const notice=ref(''),noticeLevel=ref('info'),busy=ref(false),streamReady=ref(false);
 const needsAuth=ref(false),tokenInput=ref(''),adminToken=ref('');
 const editing=ref(false),editNote=ref(''),editName=ref(''),editSource=ref('');
 const editingKey=ref(''),editingSlot=ref(0),editNameInput=ref(null);
 const visibleQueue=computed(()=>{
  const word=search.value.toLowerCase();
  return sortedQueue.value.filter(q=>!word||(String(q.username||'')+' '+String(q.note||'')+' '+String(q.mode||'')+' '+String(q.platform||'')+' '+String(q.user_id||'')+' '+String(q.source_platform||'')).toLowerCase().includes(word));
 });
 const selectedIndex=computed(()=>queue.value.findIndex(q=>q.key===selectedKey.value));
 const selectedItem=computed(()=>selectedIndex.value>=0?queue.value[selectedIndex.value]:null);
 const slotCount=computed(()=>Number(slotInfo.value.slots?.[activeSlot.value]??queue.value.length));
 const platformName=p=>({bilibili:'B站',douyin:'抖音',huya:'虎牙',wechat_mp:'微信公众号',kuaishou:'快手',douyu:'斗鱼'})[p]||p||'无来源';
 const queueSourceLabel=q=>q.platform==='manual' ? platformName(q.source_platform) : platformName(q.platform);
 const isManual=q=>!!q&&q.platform==='manual'&&/^(manual|admin):/.test(q.key||'');
 const editingManual=computed(()=>isManual(queue.value.find(q=>q.key===editingKey.value)));
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
 function reconcileSelection(){
  if(selectedKey.value&&!queue.value.some(x=>x.key===selectedKey.value))selectedKey.value='';
  if(editing.value&&(activeSlot.value!==editingSlot.value||!queue.value.some(x=>x.key===editingKey.value))){
   closeEditing();notify('当前成员已被移除或切换了存档，编辑已取消','error');
  }
 }
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
 function selectItem(key){if(selectedKey.value===key){selectedKey.value='';return;}
  selectedKey.value=key;
 }
 async function mutate(path,payload){
  if(busy.value)return false;
  busy.value=true;++readSeq;
  try{
   const data=await api(path,{method:'POST',body:JSON.stringify(payload)});
   if(path==='/api/queue/slots'){
    queue.value=Array.isArray(data.entries)?data.entries:[];
    slotInfo.value=data;activeSlot.value=data.active_slot||1;
    selectedKey.value='';closeEditing();
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
  cancelSorting();const target=event.target,slot=Number(target.value);
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
 function closeEditing(){if(busy.value)return;editing.value=false;editingKey.value='';editingSlot.value=0;}
 async function startEditing(key){
  if(typeof key==='string')selectedKey.value=key;
  const item=selectedItem.value;if(!item)return;
  editingKey.value=item.key;editingSlot.value=activeSlot.value;
  editName.value=item.username||'';editNote.value=item.note||'';
  editSource.value=item.platform==='manual'?(item.source_platform||''):item.platform;
  editing.value=true;
  await nextTick();
  editNameInput.value?.focus();
 }
 async function saveEdit(){
  if(!editing.value||busy.value)return;
  if(editingSlot.value!==activeSlot.value){closeEditing();notify('存档已切换，请重新选择成员','error');return;}
  const current=queue.value.find(q=>q.key===editingKey.value);
  if(!current){closeEditing();notify('成员已不存在，请刷新后重试','error');return;}
  const payload={action:'edit',key:editingKey.value,note:editNote.value};
  if(isManual(current)){
   payload.new_name=editName.value.trim();payload.source_platform=editSource.value;
  }
  const ok=await mutate('/api/queue',payload);
  if(ok)closeEditing();
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
 function keyHandler(event){if(event.key==='Escape'&&editing.value&&!busy.value){closeEditing();}}
 function connectSSE(){
  events=new EventSource('/api/events');
  events.onopen=()=>streamReady.value=true;
  events.onerror=()=>streamReady.value=false;
  events.onmessage=e=>{try{const msg=JSON.parse(e.data);if(msg.type==='queue')void refresh();}catch{}};
 }
 onMounted(()=>{void loadAppearance();void refresh();connectSSE();pollTimer=setInterval(refresh,8000);window.addEventListener('keydown',keyHandler);});
 onUnmounted(()=>{events?.close();clearInterval(pollTimer);window.removeEventListener('keydown',keyHandler);});
 return {sorting,sortedQueue,sortBefore,sortKeys,dragging,beginSorting,cancelSorting,dragStart,dragOver,dragDrop,saveSorting,queue,slotInfo,activeSlot,selectedKey,selectedIndex,selectedItem,slotCount,visibleQueue,search,newName,notice,noticeLevel,
  busy,streamReady,needsAuth,tokenInput,saveToken,editing,editNote,editName,editSource,editNameInput,editingManual,closeEditing,position,platformName,queueSourceLabel,timeOf,
  refresh,selectItem,switchSlot,addQueue,insertQueue,moveSelected,startEditing,saveEdit,removeSelected,completeFirst,clearQueue};
}}).mount('#app');
