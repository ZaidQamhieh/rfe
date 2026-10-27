import { recentOptions, recentRouteParams } from './recentRoot';

describe('recentOptions', () => {
  it('asks for the whole host when no folder is given', () => {
    expect(recentOptions(undefined)).toEqual({});
    expect(recentOptions('')).toEqual({});
  });
  it('limits the request to the folder when one is given', () => {
    expect(recentOptions('/home/x/docs')).toEqual({ root: '/home/x/docs' });
  });
});

describe('recentRouteParams', () => {
  it('carries the folder as the route param the Recent screen reads', () => {
    expect(recentRouteParams('/home/x/docs')).toEqual({ root: '/home/x/docs' });
  });
});
