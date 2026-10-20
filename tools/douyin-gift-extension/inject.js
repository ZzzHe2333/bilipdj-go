// Runs in the page MAIN world. Does not read cookies, generate signatures,
// send gifts, or modify messages. Copy only server binary frames containing
// WebcastGiftMessage to the isolated extension bridge.
(() => {
  'use strict';
  if (window.__pdjDouyinGiftHook) return;
  window.__pdjDouyinGiftHook = true;
  const NativeWS = window.WebSocket;
  if (!NativeWS) return;
  const match = /\/webcast\/im\/push\//;

  function varint(bytes, start) {
    let value = 0, scale = 1, p = start;
    while (p < bytes.length && scale <= 0x20000000000000) {
      const b = bytes[p++];
      value += (b & 127) * scale;
      if (b < 128) return [value, p];
      scale *= 128;
    }
    return [null, bytes.length];
  }
  function payload(bytes) {
    let p = 0;
    while (p < bytes.length) {
      let key; [key, p] = varint(bytes, p);
      if (key == null) return null;
      const f = Math.floor(key / 8), wire = key % 8;
      if (wire === 0) { [, p] = varint(bytes, p); }
      else if (wire === 1) p += 8;
      else if (wire === 5) p += 4;
      else if (wire === 2) {
        let len; [len, p] = varint(bytes, p);
        if (len == null || p + len > bytes.length) return null;
        if (f === 8) return bytes.subarray(p, p + len);
        p += len;
      } else return null;
    }
    return null;
  }
  function toBase64(bytes) {
    const chunks = [];
    for (let p = 0; p < bytes.length; p += 0x4000) {
      chunks.push(String.fromCharCode(...bytes.subarray(p, p + 0x4000)));
    }
    return btoa(chunks.join(''));
  }
  async function relay(data) {
    const buf = data instanceof Blob ? await data.arrayBuffer() : data;
    if (!(buf instanceof ArrayBuffer) || !buf.byteLength || buf.byteLength > 1048576) return;
    const raw = new Uint8Array(buf);
    const body = payload(raw);
    if (!body || !body.length) return;
    let decoded = body;
    if (body[0] === 0x1f && body[1] === 0x8b) {
      try {
        decoded = new Uint8Array(await new Response(
          new Blob([body]).stream().pipeThrough(new DecompressionStream('gzip'))
        ).arrayBuffer());
      } catch { return; }
    }
    if (decoded.length > 4194304) return;
    // Protobuf method is a literal UTF-8 string within Response.
    if (!new TextDecoder().decode(decoded).includes('WebcastGiftMessage')) return;
    const liveID = location.pathname.split('/').filter(Boolean)[0] || '';
    if (!/^[0-9A-Za-z_-]{1,100}$/.test(liveID)) return;
    window.postMessage({source:'pdj-douyin-wss-frame-v1',live_id:liveID,frame:toBase64(raw)}, location.origin);
  }
  function hook(ws) {
    if (ws.__pdjHooked || !match.test(ws.url || '')) return;
    ws.__pdjHooked = true;
    ws.addEventListener('message', e => {
      if (e.data instanceof ArrayBuffer || e.data instanceof Blob) relay(e.data).catch(()=>{});
    });
  }
  function HookWS(url, protocols) {
    const ws = protocols === undefined ? new NativeWS(url) : new NativeWS(url, protocols);
    hook(ws);
    return ws;
  }
  HookWS.prototype = NativeWS.prototype;
  for(const k of ['CONNECTING','OPEN','CLOSING','CLOSED']) HookWS[k] = NativeWS[k];
  window.WebSocket = HookWS;
  const nativeAdd = NativeWS.prototype.addEventListener;
  NativeWS.prototype.addEventListener = function(type, listener, options) {
    if (type === 'message') hook(this);
    return nativeAdd.call(this, type, listener, options);
  };
})();
