import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

interface UnsavedConfigGuardDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  reason?: string
  busy?: boolean
  onDiscard: () => void | Promise<void>
}

export function UnsavedConfigGuardDialog({ open, onOpenChange, reason, busy = false, onDiscard }: UnsavedConfigGuardDialogProps) {
  const { t } = useTranslation()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('configGuard.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {reason ? t('configGuard.descriptionWithReason', { reason }) : t('configGuard.description')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>{t('configGuard.continueEditing')}</AlertDialogCancel>
          <AlertDialogAction
            disabled={busy}
            onClick={(event) => {
              // Keep the controlled dialog open until discard and navigation
              // both settle; a failure must leave the user on the editor.
              event.preventDefault()
              void onDiscard()
            }}
          >
            {t('configGuard.discardChanges')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
