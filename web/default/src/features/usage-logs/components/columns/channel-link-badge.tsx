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
import { StatusBadge } from '@/components/status-badge'
import { normalizeExternalUrl } from '@/lib/external-url'

type ChannelLinkBadgeProps = {
  channelId: number
  baseUrl?: string | null
}

export function ChannelLinkBadge({
  channelId,
  baseUrl,
}: ChannelLinkBadgeProps) {
  const label = `#${channelId}`
  const href = normalizeExternalUrl(baseUrl || undefined)
  const badge = (
    <StatusBadge
      label={label}
      autoColor={String(channelId)}
      copyable={false}
      size='sm'
      showDot={false}
      className='font-mono'
      title={href || label}
    />
  )

  if (!href) return badge

  return (
    <a
      href={href}
      target='_blank'
      rel='noopener noreferrer'
      className='decoration-foreground/30 hover:decoration-foreground focus-visible:ring-ring inline-flex w-fit underline decoration-1 underline-offset-4 transition-colors focus-visible:ring-2 focus-visible:outline-none'
      title={href}
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => event.stopPropagation()}
    >
      {badge}
    </a>
  )
}
