// This content script runs isolated from the webpage; pairing credentials
// live only in the extension service worker/storage, never in window state.
window.addEventListener('message', event => {
  if (event.source !== window || event.origin !== 'https://live.douyin.com') return;
  const payload = event.data;
  if (!payload || payload.source !== 'pdj-douyin-wss-frame-v1') return;
  if (typeof payload.live_id !== 'string' || typeof payload.frame !== 'string') return;
  if (payload.frame.length > 1450000) return;
  chrome.runtime.sendMessage({type:'pdj-douyin-frame',live_id:payload.live_id,frame:payload.frame})
    .catch(() => {});
});
