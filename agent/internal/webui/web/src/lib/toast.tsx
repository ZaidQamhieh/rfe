import { useCallback, useMemo, useRef, useState, type ReactNode } from 'react';
import { Check } from 'lucide-react';
import { ToastContext } from './toast-context';

/** How long a toast stays on screen, in milliseconds. */
const TOAST_MS = 2200;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [msg, setMsg] = useState('');
  const [show, setShow] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Stable, so showing a toast does not re-render every page that reads the context.
  const toast = useCallback((m: string) => {
    setMsg(m);
    setShow(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setShow(false), TOAST_MS);
  }, []);
  const value = useMemo(() => ({ toast }), [toast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className={`toast${show ? ' show' : ''}`}>
        <Check />
        <span>{msg}</span>
      </div>
    </ToastContext.Provider>
  );
}
