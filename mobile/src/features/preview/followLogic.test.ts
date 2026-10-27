import { completeLines, FOLLOW_CHUNK_BYTES, followChunk, followStep, utf8SafeLength } from './followLogic';

const bytes = (s: string) => new TextEncoder().encode(s);

describe('followStep', () => {
  it('does nothing while the file has not grown', () => {
    expect(followStep(100, 100)).toEqual({ kind: 'idle' });
  });
  it('asks for exactly the new bytes when it grew', () => {
    expect(followStep(100, 160)).toEqual({ kind: 'append', range: 'bytes=100-159' });
  });
  it('caps one request, so a huge jump arrives over several polls', () => {
    expect(followStep(0, FOLLOW_CHUNK_BYTES * 3)).toEqual({ kind: 'append', range: `bytes=0-${FOLLOW_CHUNK_BYTES - 1}` });
  });
  it('stops when the file got shorter: it was truncated or rotated, so what is shown is stale', () => {
    expect(followStep(100, 40)).toEqual({ kind: 'truncated' });
  });
});

describe('completeLines', () => {
  it('counts bytes up to and including the last newline', () => {
    expect(completeLines(bytes('one\ntwo\npart'))).toBe(8);
    expect(completeLines(bytes('one\n'))).toBe(4);
  });
  it('is zero while a line is still being written', () => {
    expect(completeLines(bytes('partial'))).toBe(0);
    expect(completeLines(new Uint8Array())).toBe(0);
  });
});

describe('followChunk', () => {
  it('returns whole lines only and how many bytes they took, leaving the partial tail for the next poll', () => {
    expect(followChunk(bytes('a\nb\nc'))).toEqual({ text: 'a\nb\n', consumed: 4 });
  });
  it('never splits a multi-byte character: it only cuts at a newline', () => {
    const all = bytes('héllo\nwörld\n');
    const cut = all.slice(0, 3); // ends in the middle of "é"
    expect(followChunk(cut)).toEqual({ text: '', consumed: 0 });
    expect(followChunk(all)).toEqual({ text: 'héllo\nwörld\n', consumed: all.length });
  });
  it('rejects binary data instead of showing garbage', () => {
    expect(() => followChunk(new Uint8Array([0x61, 0x00, 0x0a]))).toThrow();
  });
});

describe('a full chunk with no newline', () => {
  it('is taken up to a character boundary so a very long line cannot stall following', () => {
    const long = new Uint8Array(FOLLOW_CHUNK_BYTES).fill(0x61);
    const r = followChunk(long);
    expect(r.consumed).toBe(FOLLOW_CHUNK_BYTES);
    expect(r.text.length).toBe(FOLLOW_CHUNK_BYTES);
  });
  it('does not cut a multi-byte character at the end of the chunk', () => {
    const e = bytes('é'); // 2 bytes
    const chunk = new Uint8Array(FOLLOW_CHUNK_BYTES).fill(0x61);
    chunk.set(e.slice(0, 1), FOLLOW_CHUNK_BYTES - 1); // first half of "é" is the last byte
    expect(followChunk(chunk).consumed).toBe(FOLLOW_CHUNK_BYTES - 1);
  });
  it('still waits for a newline when the chunk is short', () => {
    expect(followChunk(bytes('no newline yet')).consumed).toBe(0);
  });
});

describe('utf8SafeLength', () => {
  it('drops an incomplete trailing sequence and keeps complete ones', () => {
    expect(utf8SafeLength(bytes('aé'))).toBe(3);
    expect(utf8SafeLength(bytes('aé').slice(0, 2))).toBe(1);
    expect(utf8SafeLength(bytes('a€').slice(0, 3))).toBe(1);
    expect(utf8SafeLength(bytes('a'))).toBe(1);
  });
});
