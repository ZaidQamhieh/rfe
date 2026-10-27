import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react';

import { ConfirmDialog } from './Dialog';
import { ChoiceDialog, PromptDialog, ReportDialog, type ChoiceOption } from './Prompts';

type Prompt = { title: string; description?: string; placeholder?: string; initialValue?: string; confirmLabel: string; allowEmpty?: boolean; mono?: boolean; helper?: string; validate?: (value: string) => string | null; keyboardType?: 'default' | 'url' | 'number-pad' };
type Choose<T extends string> = { title: string; subtitle?: string; options: ChoiceOption<T>[]; icon?: ReactNode; tint?: string };
type Confirm = { title: string; description: string; confirmLabel: string; cancelLabel: string; destructive?: boolean };
type Report = { title: string; items: { primary: string; secondary?: string }[] };

type Api = {
  /** Resolves with the trimmed text, or null if cancelled. */
  prompt(o: Prompt): Promise<string | null>;
  /** Resolves with the chosen value, or null if dismissed. */
  choose<T extends string>(o: Choose<T>): Promise<T | null>;
  confirm(o: Confirm): Promise<boolean>;
  report(o: Report): Promise<void>;
};

const Ctx = createContext<Api | null>(null);

export function useDialogs(): Api {
  const a = useContext(Ctx);
  if (!a) throw new Error('DialogHost missing');
  return a;
}

type Active =
  | { kind: 'prompt'; o: Prompt; resolve: (v: string | null) => void }
  | { kind: 'choose'; o: Choose<string>; resolve: (v: string | null) => void }
  | { kind: 'confirm'; o: Confirm; resolve: (v: boolean) => void }
  | { kind: 'report'; o: Report; resolve: () => void };

/** Imperative, promise-based dialogs so ported flows read like the Dart `await showDialog(...)` code. One dialog at a time, queued. */
export function DialogHost({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<Active | null>(null);
  const queue = useRef<Active[]>([]);
  const next = useCallback(() => setActive(queue.current.shift() ?? null), []);
  const push = useCallback((a: Active) => {
    setActive((cur) => {
      if (cur) {
        queue.current.push(a);
        return cur;
      }
      return a;
    });
  }, []);

  const api = useMemo<Api>(
    () => ({
      prompt: (o) => new Promise((resolve) => push({ kind: 'prompt', o, resolve })),
      choose: <T extends string>(o: Choose<T>) => new Promise<T | null>((resolve) => push({ kind: 'choose', o: o as Choose<string>, resolve: resolve as (v: string | null) => void })),
      confirm: (o) => new Promise((resolve) => push({ kind: 'confirm', o, resolve })),
      report: (o) => new Promise((resolve) => push({ kind: 'report', o, resolve })),
    }),
    [push],
  );

  const done = <V,>(resolve: (v: V) => void, v: V) => {
    resolve(v);
    next();
  };
  return (
    <Ctx.Provider value={api}>
      {children}
      {active?.kind === 'prompt' && (
        <PromptDialog visible {...active.o} onSubmit={(v) => done(active.resolve, v)} onCancel={() => done(active.resolve, null)} />
      )}
      {active?.kind === 'choose' && (
        <ChoiceDialog visible {...active.o} onChoose={(v) => done(active.resolve, v)} onDismiss={() => done(active.resolve, null)} />
      )}
      {active?.kind === 'confirm' && (
        <ConfirmDialog visible {...active.o} onConfirm={() => done(active.resolve, true)} onCancel={() => done(active.resolve, false)} />
      )}
      {active?.kind === 'report' && <ReportDialog visible {...active.o} onClose={() => done(active.resolve, undefined)} />}
    </Ctx.Provider>
  );
}
