// Live MSE player: receives a codec string (text) then fMP4 init + fragments (binary) over WebSocket.
function startPlayer(video) {
  const badge = document.getElementById(video.dataset.badge);
  const setLive = (on) => {
    badge.textContent = on ? "live" : "offline";
    badge.classList.toggle("live", on);
  };

  function connect() {
    let ms = null, sb = null, queue = [], ws = null, lastData = 0;
    const url = (location.protocol === "https:" ? "wss://" : "ws://") + location.host + video.dataset.ws;
    ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";

    const retry = () => {
      clearInterval(watchdog);
      setLive(false);
      if (video.src) URL.revokeObjectURL(video.src);
      video.removeAttribute("src");
      video.load();
      setTimeout(connect, 1000);
    };

    const pump = () => {
      if (!sb || sb.updating || queue.length === 0) return;
      try {
        sb.appendBuffer(queue.shift());
      } catch (e) {
        ws.close();
      }
    };

    const onUpdateEnd = () => {
      if (sb.buffered.length) {
        const start = sb.buffered.start(0);
        const end = sb.buffered.end(sb.buffered.length - 1);
        // Stay near the live edge (~1s target) and keep the buffer small.
        if (end - video.currentTime > 1.5 || video.currentTime < start) video.currentTime = end - 0.3;
        if (video.currentTime - start > 5 && !sb.updating) {
          sb.remove(start, video.currentTime - 2);
          return;
        }
      }
      if (video.paused) video.play().catch(() => {});
      pump();
    };

    ws.onmessage = (ev) => {
      lastData = Date.now();
      if (typeof ev.data === "string") {
        const type = `video/mp4; codecs="${ev.data}"`;
        if (!window.MediaSource || !MediaSource.isTypeSupported(type)) return ws.close();
        queue = [];
        ms = new MediaSource();
        video.src = URL.createObjectURL(ms);
        ms.addEventListener("sourceopen", () => {
          sb = ms.addSourceBuffer(type);
          sb.mode = "sequence";
          sb.addEventListener("updateend", onUpdateEnd);
          setLive(true);
          pump();
        }, { once: true });
        return;
      }
      queue.push(ev.data);
      if (queue.length > 60) queue.splice(0, queue.length - 30);
      pump();
    };

    // Reconnect if the stream stalls (e.g. camera went away).
    const watchdog = setInterval(() => {
      if (lastData && Date.now() - lastData > 5000) ws.close();
    }, 2000);
    ws.onclose = retry;
    ws.onerror = () => ws.close();
  }
  connect();
}

document.querySelectorAll("video[data-ws]").forEach(startPlayer);
