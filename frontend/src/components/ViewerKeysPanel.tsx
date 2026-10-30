import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Copy, Eye, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api, type AdminViewerKey } from '../api'
import { useToast } from '../hooks/useToast'
import { useConfirmDialog } from '../hooks/useConfirmDialog'
import { formatBeijingTime } from '../utils/time'
import { getErrorMessage } from '../utils/error'
import Modal from './Modal'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Input } from './ui/input'

/**
 * Read-only access keys (viewer role). Holders can sign in to see the dashboard,
 * the account pool and usage stats, and nothing else. Lives in Settings ›
 * general › security; the backend lets only full admins reach /viewer-keys.
 */
export default function ViewerKeysPanel() {
  const { t } = useTranslation()
  const { showToast } = useToast()
  const { confirm, confirmDialog } = useConfirmDialog()
  const [keys, setKeys] = useState<AdminViewerKey[]>([])
  const [loading, setLoading] = useState(true)
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<{ name: string; key: string } | null>(null)
  const [copied, setCopied] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listViewerKeys()
      setKeys(res.keys ?? [])
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }, [showToast])

  useEffect(() => {
    void load()
  }, [load])

  const handleCreate = async () => {
    const trimmed = name.trim()
    if (!trimmed || creating) return
    setCreating(true)
    try {
      const res = await api.createViewerKey(trimmed)
      setCreated({ name: res.name, key: res.key })
      setCopied(false)
      setName('')
      void load()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setCreating(false)
    }
  }

  const handleRevoke = async (key: AdminViewerKey) => {
    const ok = await confirm({
      title: t('viewerKeys.revokeTitle'),
      description: t('viewerKeys.revokeDesc', { name: key.name }),
      confirmText: t('viewerKeys.revoke'),
      tone: 'destructive',
      confirmVariant: 'destructive',
    })
    if (!ok) return
    try {
      await api.revokeViewerKey(key.id)
      showToast(t('viewerKeys.revoked'))
      void load()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    }
  }

  const handleCopy = async () => {
    if (!created) return
    try {
      await navigator.clipboard.writeText(created.key)
      setCopied(true)
    } catch {
      showToast(t('viewerKeys.copyFailed'), 'error')
    }
  }

  const active = keys.filter((key) => !key.revoked)
  const revoked = keys.filter((key) => key.revoked)

  return (
    <div className="space-y-4">
      <p className="text-xs leading-relaxed text-muted-foreground">{t('viewerKeys.intro')}</p>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="max-w-xs"
          placeholder={t('viewerKeys.namePlaceholder')}
          value={name}
          maxLength={64}
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') void handleCreate() }}
        />
        <Button size="sm" disabled={!name.trim() || creating} onClick={() => void handleCreate()}>
          {creating ? <RefreshCw className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
          {t('viewerKeys.create')}
        </Button>
      </div>

      <div className="overflow-hidden rounded-lg border border-border">
        {loading && keys.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">{t('common.loading')}</div>
        ) : keys.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">{t('viewerKeys.empty')}</div>
        ) : (
          <ul className="divide-y divide-border">
            {[...active, ...revoked].map((key) => (
              <li key={key.id} className={`flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2.5 text-sm ${key.revoked ? 'opacity-55' : ''}`}>
                <span className="flex min-w-0 items-center gap-2 font-medium">
                  <Eye className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{key.name}</span>
                </span>
                <span className="font-mono text-xs text-muted-foreground">{key.key_prefix}…</span>
                <span className="text-xs text-muted-foreground">
                  {t('viewerKeys.createdAt')} {formatBeijingTime(key.created_at)}
                </span>
                <span className="text-xs text-muted-foreground">
                  {t('viewerKeys.lastUsed')} {key.last_used_at ? formatBeijingTime(key.last_used_at) : t('viewerKeys.never')}
                </span>
                <span className="ml-auto">
                  {key.revoked ? (
                    <Badge variant="outline" className="text-[11px]">{t('viewerKeys.revokedBadge')}</Badge>
                  ) : (
                    <Button variant="ghost" size="sm" className="text-destructive hover:bg-destructive/10 hover:text-destructive" onClick={() => void handleRevoke(key)}>
                      <Trash2 className="size-3.5" />
                      {t('viewerKeys.revoke')}
                    </Button>
                  )}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <Modal
        show={created !== null}
        title={t('viewerKeys.createdTitle', { name: created?.name ?? '' })}
        onClose={() => setCreated(null)}
        footer={<Button onClick={() => setCreated(null)}>{t('viewerKeys.saved')}</Button>}
      >
        <p className="mb-3 text-sm text-muted-foreground">{t('viewerKeys.createdDesc')}</p>
        <div className="flex items-center gap-2 rounded-lg border border-dashed border-input bg-muted/40 px-3 py-2.5">
          <code className="min-w-0 flex-1 truncate font-mono text-xs text-foreground">{created?.key}</code>
          <Button variant="outline" size="sm" onClick={() => void handleCopy()}>
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            {copied ? t('viewerKeys.copied') : t('viewerKeys.copy')}
          </Button>
        </div>
      </Modal>
      {confirmDialog}
    </div>
  )
}
