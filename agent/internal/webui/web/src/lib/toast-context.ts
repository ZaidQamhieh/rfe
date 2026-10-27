import { createContext, useContext } from 'react';

interface ToastState {
  toast: (msg: string) => void;
}

export const ToastContext = createContext<ToastState | null>(null);

export function useToast(): ToastState {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error('useToast must be used inside ToastProvider');
  return ctx;
}
