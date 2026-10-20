// Injected at document-start by the bundled Douyin CDP collector.
// Only reads browser-owned WebSocket frames; never reads credentials or sends gifts.
(() => {
  'use strict';
  if (window.__pdjCollectorHook) return;
  window.__pdjCollectorHook = true;
  const NativeWS = window.WebSocket;
  const queue = [];
  let imFrames = 0, lastIM = 0, skipped = 0, errors = 0;
  const MAX_QUEUE = 160;
  const decoder = new TextDecoder();

  window.__pdjCollectorDrain = () => {
    const out = queue.splice(0, queue.length);
    return {frames:out, lastIM:lastIM, imFrames:imFrames, skipped:skipped, errors:errors};
  };

  function varint(buf, start) {
    let n = 0, power = 1, p = start;
    for (let i = 0; i < 10 && p < buf.length; i++) {
      const b = buf[p++];
      n += (b & 127) * power;
      if (b < 128) return [n,p];
      power *= 128;
      if (!Number.isSafeInteger(power)) break;
    }
    return [null,buf.length];
  }

  function extractFramePayload(frame) {
    let p = 0;
    while (p < frame.length) {
      let tag; [tag,p] = varint(frame,p);
      if (tag === null) return null;
      const field = Math.floor(tag/8), wire=tag%8;
      if (wire === 0) {
        const result=varint(frame,p); if(result[0]===null)return null; p=result[1];
      } else if (wire === 1) p+=8;
      else if (wire === 5) p+=4;
      else if (wire === 2) {
        let length; [length,p] = varint(frame,p);
        if(length===null || length<0 || p+length>frame.length)return null;
        if(field===8)return frame.subarray(p,p+length);
        p+=length;
      } else return null;
    }
    return null;
  }

  function encodeBase64(raw) {
    let s='';
    for(let i=0;i<raw.length;i+=16384) {
      s+=String.fromCharCode(...raw.subarray(i,i+16384));
    }
    return btoa(s);
  }

  async function checkFrame(data) {
    try {
      const buf = data instanceof Blob ? await data.arrayBuffer() : data;
      if(!(buf instanceof ArrayBuffer) || buf.byteLength > 1048576 || !buf.byteLength)return;
      imFrames++;
      lastIM=Date.now();
      const raw=new Uint8Array(buf);
      const payload=extractFramePayload(raw);
      if(!payload)return;
      let decoded=payload;
      if(payload[0]===31 && payload[1]===139) {
        decoded=new Uint8Array(await new Response(
          new Blob([payload]).stream().pipeThrough(new DecompressionStream('gzip'))
        ).arrayBuffer());
      }
      if(decoded.length>4194304)return;
      if(!decoder.decode(decoded).includes('WebcastGiftMessage'))return;
      if(queue.length >= MAX_QUEUE) {queue.shift();skipped++;}
      queue.push(encodeBase64(raw));
    } catch {errors++;}
  }

  function hook(ws) {
    if(ws.__pdjCollectorWS || !/\/webcast\/im\//.test(ws.url||''))return;
    ws.__pdjCollectorWS=true;
    ws.addEventListener('message',(ev)=>checkFrame(ev.data));
  }
  function WrappedWS(url,protocols) {
    const ws = protocols===undefined ? new NativeWS(url) : new NativeWS(url,protocols);
    hook(ws);
    return ws;
  }
  WrappedWS.prototype=NativeWS.prototype;
  Object.setPrototypeOf(WrappedWS,NativeWS);
  window.WebSocket=WrappedWS;
})();
