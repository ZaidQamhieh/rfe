import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { UserPlus, Trash2, Users as UsersIcon } from 'lucide-react';
import { api, ApiError, type UserAccount } from '@/lib/api';
import { useToast } from '@/lib/toast-context';
import { Dialog } from '@/components/Dialog';

function since(created: number): string {
  return `since ${new Date(created * 1000).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })}`;
}

const AVATAR_GRADIENTS = [
  'linear-gradient(135deg,var(--primary),var(--violet))',
  'linear-gradient(135deg,var(--amber),#d98a1f)',
  'linear-gradient(135deg,var(--green),#1f9d73)',
  'linear-gradient(135deg,var(--red),#c23a51)',
];
function avatarGradient(username: string): string {
  let hash = 0;
  for (let i = 0; i < username.length; i++) hash = (hash * 31 + username.charCodeAt(i)) | 0;
  return AVATAR_GRADIENTS[Math.abs(hash) % AVATAR_GRADIENTS.length];
}

export function Users() {
  const { toast } = useToast();
  const qc = useQueryClient();
  const { data: users, isLoading } = useQuery({ queryKey: ['users'], queryFn: api.listUsers });
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [showAddInfo, setShowAddInfo] = useState(false);

  const deleteMutation = useMutation({
    mutationFn: (username: string) => api.deleteUser(username),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] });
      toast('User removed');
    },
    onError: (err) => {
      toast(err instanceof ApiError ? err.message : 'Failed to remove user');
    },
    onSettled: () => setPendingDelete(null),
  });

  function onRemoveClick(u: UserAccount) {
    if (pendingDelete === u.username) {
      deleteMutation.mutate(u.username);
    } else {
      setPendingDelete(u.username);
    }
  }

  if (isLoading) return <div className="empty"><p>Loading users…</p></div>;

  const list = users ?? [];
  const isLastUser = list.length <= 1;

  return (
    <div>
      <div className="panel-head">
        <div className="icon-badge amber">
          <UsersIcon />
        </div>
        <h3>Users</h3>
      </div>
      <div className="toolbar">
        <div className="grow" />
        <button className="btn btn-primary btn-sm" onClick={() => setShowAddInfo(true)}>
          <UserPlus /> Add user
        </button>
      </div>

      {list.length === 0 ? (
        <div className="empty">
          <h4>No users</h4>
          <p>No accounts were found on this agent.</p>
        </div>
      ) : (
        <div className="grid grid-3">
          {list.map((u) => (
            <div className="card" key={u.username}>
              <div className="who-avatar" style={{ background: avatarGradient(u.username) }}>{u.username.slice(0, 1).toUpperCase()}</div>
              <div style={{ fontSize: 13, fontWeight: 600, marginTop: 10 }}>{u.username}</div>
              <div style={{ fontSize: 11.5, color: 'var(--text-faint)', marginTop: 2 }}>{since(u.created)}</div>
              <div style={{ marginTop: 10, display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className="badge blue">Admin</span>
                <button
                  className={`btn ${pendingDelete === u.username ? 'btn-danger' : 'btn-ghost'} btn-sm`}
                  style={{ marginLeft: 'auto' }}
                  disabled={isLastUser || deleteMutation.isPending}
                  onClick={() => onRemoveClick(u)}
                >
                  <Trash2 /> {pendingDelete === u.username ? 'Confirm?' : 'Remove'}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      <Dialog open={showAddInfo} onOpenChange={setShowAddInfo} title="Add user">
        <p style={{ fontSize: 12.5, color: 'var(--text-dim)', lineHeight: 1.6, margin: 0 }}>
          There's no "add another login" form here — additional accounts are created via{' '}
          <code className="mono">rfe adduser</code> on the host, or by pairing a new device with a fresh
          pairing code through the Register flow.
        </p>
      </Dialog>
    </div>
  );
}
