// Minimal protobuf wire encoder for the synthetic Feishu server's pbbp2.Frame.
// The client still uses the official SDK's decoder and event dispatcher.
function uint(value) {
  const result = [];
  do {const byte = value & 127; value = Math.floor(value / 128); result.push(byte | (value ? 128 : 0));} while (value);
  return Buffer.from(result);
}
function bytes(field, value) {
  const data = Buffer.isBuffer(value) ? value : Buffer.from(value);
  return Buffer.concat([uint(field * 8 + 2), uint(data.length), data]);
}
export function eventFrame(sequence, id, payload) {
  const headers = {type: 'event', message_id: id, sum: '1', seq: '0', trace_id: id};
  return Buffer.concat([
    Buffer.from([8]), uint(sequence), Buffer.from([16]), uint(sequence), Buffer.from([24, 1, 32, 1]),
    ...Object.entries(headers).map(([key, value]) => bytes(5, Buffer.concat([bytes(1, key), bytes(2, value)]))),
    bytes(7, 'application/json'), bytes(8, JSON.stringify(payload)),
  ]);
}
