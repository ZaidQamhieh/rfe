import { useRef, useState, type ReactNode } from 'react';
import { Check } from 'lucide-react';
import { ToastContext } from './toast-context';

export function ToastProvider({ children }: { children: ReactNode }) {
  const [msg, setMsg] = useState('');
  const [show, setShow] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const toast = (m: string) => {
    setMsg(m);
    setShow(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setShow(false), 2200);
  };

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      <div className={`toast${show ? ' show' : ''}`}>
        <Check />
        <span>{msg}</span>
      </div>
    </ToastContext.Provider>
  );
}
