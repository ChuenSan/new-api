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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Skeleton } from '@/components/ui/skeleton'

import type { AvailableChannel } from '../types'

type ChannelAccessMode = 'all' | 'specific'

type ApiKeyChannelSelectorProps = {
  mode: ChannelAccessMode
  selected: number[]
  channels: AvailableChannel[]
  isLoading: boolean
  isError: boolean
  onModeChange: (mode: ChannelAccessMode) => void
  onSelectedChange: (ids: number[]) => void
}

function modelSummary(models: string) {
  const values = models
    .split(',')
    .map((model) => model.trim())
    .filter(Boolean)
  if (values.length <= 3) return values.join(', ') || '-'
  return `${values.slice(0, 3).join(', ')}…`
}

export function ApiKeyChannelSelector({
  mode,
  selected,
  channels,
  isLoading,
  isError,
  onModeChange,
  onSelectedChange,
}: ApiKeyChannelSelectorProps) {
  const { t } = useTranslation()
  const selectedSet = new Set(selected)
  const allSelected =
    channels.length > 0 &&
    channels.every((channel) => selectedSet.has(channel.id))

  const toggleChannel = (id: number) => {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    onSelectedChange([...next].sort((a, b) => a - b))
  }

  const selectAllChannels = () => {
    const ids = channels.map((channel) => channel.id).sort((a, b) => a - b)
    onSelectedChange(ids)
  }

  return (
    <div className='flex flex-col gap-3'>
      <RadioGroup
        value={mode}
        onValueChange={(value) => onModeChange(value as ChannelAccessMode)}
        className='grid gap-3 sm:grid-cols-2'
      >
        <Label className='has-data-[checked]:border-primary flex cursor-pointer items-start gap-3 rounded-lg border p-3 font-normal'>
          <RadioGroupItem value='all' />
          <span className='flex flex-col gap-1'>
            <span className='font-medium'>{t('All channels')}</span>
            <span className='text-muted-foreground text-xs'>
              {t(
                'Use existing model-level routing without channel restrictions'
              )}
            </span>
          </span>
        </Label>
        <Label className='has-data-[checked]:border-primary flex cursor-pointer items-start gap-3 rounded-lg border p-3 font-normal'>
          <RadioGroupItem value='specific' />
          <span className='flex flex-col gap-1'>
            <span className='font-medium'>{t('Specific channels')}</span>
            <span className='text-muted-foreground text-xs'>
              {t('Only route requests through selected channels')}
            </span>
          </span>
        </Label>
      </RadioGroup>

      {mode === 'specific' && (
        <div className='overflow-hidden rounded-lg border'>
          {isLoading && (
            <div className='flex flex-col gap-2 p-3'>
              <Skeleton className='h-8 w-full' />
              <Skeleton className='h-12 w-full' />
              <Skeleton className='h-12 w-full' />
            </div>
          )}
          {!isLoading && isError && (
            <p className='text-destructive p-4 text-sm'>
              {t('Failed to load enabled channels')}
            </p>
          )}
          {!isLoading && !isError && (
            <Command>
              <div className='flex items-center justify-between gap-3 border-b px-3 py-2'>
                <span className='text-muted-foreground text-xs'>
                  {t('{{count}} selected', { count: selected.length })}
                </span>
                <div className='flex items-center gap-1'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='xs'
                    disabled={channels.length === 0 || allSelected}
                    onClick={selectAllChannels}
                  >
                    {t('Select all')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='xs'
                    disabled={selected.length === 0}
                    onClick={() => onSelectedChange([])}
                  >
                    {t('Clear selection')}
                  </Button>
                </div>
              </div>
              <CommandInput placeholder={t('Search channels...')} />
              <CommandList className='max-h-64'>
                <CommandEmpty>{t('No enabled channels found')}</CommandEmpty>
                <CommandGroup>
                  {channels.map((channel) => {
                    const checked = selectedSet.has(channel.id)
                    return (
                      <CommandItem
                        key={channel.id}
                        value={`${channel.id} ${channel.name} ${channel.models}`}
                        data-checked={checked}
                        onSelect={() => toggleChannel(channel.id)}
                        className='items-start py-2 [&>svg:last-child]:hidden'
                      >
                        <Checkbox
                          checked={checked}
                          onClick={(event) => event.stopPropagation()}
                          onCheckedChange={() => toggleChannel(channel.id)}
                          aria-label={t('Select channel #{{id}}', {
                            id: channel.id,
                          })}
                          className='mt-0.5'
                        />
                        <span className='min-w-0 flex-1'>
                          <span className='flex items-baseline gap-2'>
                            <span className='text-muted-foreground font-mono text-xs'>
                              #{channel.id}
                            </span>
                            <span className='truncate font-medium'>
                              {channel.name}
                            </span>
                          </span>
                          <span className='text-muted-foreground mt-0.5 block truncate font-mono text-xs'>
                            {modelSummary(channel.models)}
                          </span>
                        </span>
                      </CommandItem>
                    )
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          )}
        </div>
      )}
    </div>
  )
}
