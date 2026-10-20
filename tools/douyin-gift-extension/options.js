const $ = id => document.getElementById(id);
chrome.storage.local.get(['pdjToken','pdjPort','pdjInstance','pdjRoomMap']).then(c => {
  $('port').value = c.pdjPort || 9816;
  $('instance').value = c.pdjInstance || 'douyin';
  $('token').value = c.pdjToken || '';
  $('roomMap').value = c.pdjRoomMap || '';
});
$('save').addEventListener('click', async () => {
  const port = Number($('port').value);
  const instance = $('instance').value.trim();
  const token = $('token').value.trim();
  if (!Number.isInteger(port) || port < 1 || port > 65535 || !/^[a-z0-9-]{1,60}$/.test(instance) || token.length < 20) {
    $('status').textContent = '端口、实例 ID 或令牌无效';
    return;
  }
  const roomMap = $('roomMap').value.trim();
  if (roomMap.split(/\n+/).some(line => line.trim() && !/^[0-9A-Za-z_-]{1,100}=[a-z0-9-]{1,60}$/.test(line.trim()))) {
    $('status').textContent = '多房间映射格式错误';
    return;
  }
  await chrome.storage.local.set({pdjPort:port,pdjInstance:instance,pdjToken:token,pdjRoomMap:roomMap});
  $('status').textContent = '已保存；刷新抖音直播网页生效';
});
