// Only this adapter chooses the transport. app.js uses the same data contract
// in the MyGo WebView and in a browser served by the ordinary HTTP preview.
const base = location.protocol === 'mygo:' ? 'probeapi://localhost' : '/api';

async function read(path, options = {}) {
  const response = await fetch(base + path, options);
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

export const host = {
  record: id => read('/record/' + id),
  body: id => read('/body/' + encodeURIComponent(id)),
  status: () => read('/status'),
  clear: () => read('/clear', { method: 'POST' }),
  recording: enabled => read('/recording', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled }),
  }),
  async watch(receive, connection, signal) {
    while (!signal.aborted) {
      try {
        const response = await fetch(base + '/watch', { signal });
        if (!response.ok) throw new Error(await response.text());
        connection(true);
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        for (;;) {
          const part = await reader.read();
          if (part.done) break;
          buffer += decoder.decode(part.value, { stream: true });
          let newline;
          while ((newline = buffer.indexOf('\n')) >= 0) {
            const line = buffer.slice(0, newline);
            buffer = buffer.slice(newline + 1);
            if (line) receive(JSON.parse(line));
          }
        }
      } catch (error) {
        if (!signal.aborted) connection(false, error.message);
      }
      if (!signal.aborted) await new Promise(resolve => setTimeout(resolve, 250));
    }
  },
};
