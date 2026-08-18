/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import { USAGE_LOGS_REFRESH_INTERVAL_SECONDS } from '../constants'
import { useUsageLogsContext } from './usage-logs-provider'

export function UsageLogsAutoRefresh() {
  const { t } = useTranslation()
  const { autoRefresh, setAutoRefresh } = useUsageLogsContext()

  return (
    <div
      className='text-muted-foreground inline-flex items-center gap-2 text-xs'
      aria-live='polite'
    >
      <RefreshCw
        className={cn('size-3.5', autoRefresh && 'animate-spin')}
        aria-hidden='true'
      />
      <span className='hidden sm:inline'>
        {autoRefresh
          ? t('Auto-refreshing every {{seconds}}s', {
              seconds: USAGE_LOGS_REFRESH_INTERVAL_SECONDS,
            })
          : t('Auto refresh')}
      </span>
      <Switch
        checked={autoRefresh}
        onCheckedChange={setAutoRefresh}
        aria-label={t('Auto refresh')}
        size='sm'
      />
    </div>
  )
}
