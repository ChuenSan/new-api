/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useNavigate } from '@tanstack/react-router'
import { Crosshair } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type { UsageLog } from '../data/schema'

// Inline action on a common log row: jump to model-route metrics and preselect
// the row for this channel × requested model. SUPER_ADMIN only — the target
// route is guarded to that role, so non-SUPER_ADMIN would just bounce to /403.
export function MetricsPreselectAction({ log }: { log: UsageLog }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const isSuperAdmin = useAuthStore(
    (s) => s.auth.user?.role === ROLE.SUPER_ADMIN
  )

  if (!isSuperAdmin) return null

  const disabled = log.channel <= 0 || !log.model_name

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size='icon-sm'
            disabled={disabled}
            aria-label={t('Select in model route')}
            onClick={(e) => {
              e.stopPropagation()
              navigate({
                to: '/model-route',
                search: {
                  tab: 'metrics',
                  channelId: log.channel,
                  model: log.model_name,
                },
              })
            }}
          />
        }
      >
        <Crosshair className='size-3.5' aria-hidden='true' />
      </TooltipTrigger>
      <TooltipContent>{t('Select in model route')}</TooltipContent>
    </Tooltip>
  )
}
