import * as RadixDialog from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import type { ReactNode } from 'react';

// Shared modal primitive (shadcn/ui-style: thin wrapper over Radix), styled
// with the mockup's own surface/border/radius tokens so it matches every
// other surface without a separate one-off stylesheet.
export function Dialog({
  open,
  onOpenChange,
  title,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  children: ReactNode;
}) {
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="cmdk-backdrop active" style={{ zIndex: 200 }} />
        <RadixDialog.Content
          className="card"
          style={{
            position: 'fixed',
            top: '50%',
            left: '50%',
            transform: 'translate(-50%,-50%)',
            width: 420,
            maxWidth: '90vw',
            zIndex: 201,
            boxShadow: 'var(--shadow-2)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', marginBottom: 14 }}>
            <RadixDialog.Title style={{ margin: 0, fontSize: 14, fontWeight: 700 }}>{title}</RadixDialog.Title>
            <RadixDialog.Close asChild>
              <button className="iconbtn" style={{ marginLeft: 'auto' }}>
                <X style={{ width: 15, height: 15 }} />
              </button>
            </RadixDialog.Close>
          </div>
          {children}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
