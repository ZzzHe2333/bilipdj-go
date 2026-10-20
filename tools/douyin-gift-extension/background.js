// Never expose the pairing token to the page or content script.
chrome.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || msg.type !== 'pdj-douyin-frame') return;
  if (!sender.url || !sender.url.startsWith('https://live.douyin.com/')) return;
  return chrome.storage.local.get(['pdjToken','pdjPort','pdjInstance','pdjRoomMap']).then(async config => {
    const port = Number(config.pdjPort || 9816);
    const token = String(config.pdjToken || '');
    const overrides = Object.create(null);
    for (const entry of String(config.pdjRoomMap || '').split(/\n+/)) {
      const pos = entry.indexOf('=');
      if (pos > 0) overrides[entry.slice(0,pos).trim()] = entry.slice(pos+1).trim();
    }
    const instance = String(overrides[msg.live_id] || config.pdjInstance || 'douyin');
    if (!token || !Number.isInteger(port) || port < 1 || port > 65535) return;
    if (!/^[a-z0-9-]{1,60}$/.test(instance)) return;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 6000);
    try {
      await fetch('http://127.0.0.1:' + port + '/api/douyin/bridge/frame', {
        method:'POST', headers:{'Content-Type':'application/json','X-PDJ-Bridge-Token':token},
        body:JSON.stringify({instance_id:instance,live_id:msg.live_id,frame:msg.frame}),
        signal:controller.signal
      });
    } catch { /* Browser local service may not be running. */ }
    finally { clearTimeout(timeout); }
  });
});
