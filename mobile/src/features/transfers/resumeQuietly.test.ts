import { log } from '../../core/log';
import { resumeQuietly } from './autoRetry';

describe('resumeQuietly', () => {
  it('resumes every id and logs a failed resume instead of leaving a rejected promise', async () => {
    const warn = jest.spyOn(log, 'warn').mockImplementation(() => {});
    const resume = jest.fn(async (id: string) => {
      if (id === 'a') throw new Error('native down');
    });
    await expect(resumeQuietly(['a', 'b'], resume)).resolves.toBeUndefined();
    expect(resume.mock.calls.map((c) => c[0])).toEqual(['a', 'b']);
    expect(warn).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });
});
