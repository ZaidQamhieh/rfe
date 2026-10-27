import en from './en.json';
import { format, type Params } from './format';

export type StringKey = keyof typeof en;

/** English-only, like the Flutter app (app_en.arb is the only locale). */
export function t(key: StringKey, params?: Params): string {
  return format(en[key] as string, params);
}
