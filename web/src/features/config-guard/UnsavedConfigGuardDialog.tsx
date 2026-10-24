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
  onDiscard: () => void | Promise<void>
}

export function UnsavedConfigGuardDialog({ open, onOpenChange, onDiscard }: UnsavedConfigGuardDialogProps) {
  const { t } = useTranslation()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('configGuard.title')}</AlertDialogTitle>
          <AlertDialogDescription>{t('configGuard.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t('configGuard.continueEditing')}</AlertDialogCancel>
          <AlertDialogAction onClick={onDiscard}>{t('configGuard.discardChanges')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
