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

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'

import { updateRateLimitCircuitBreakerThreshold } from '../api'
import { patchMetricsRateLimitThreshold } from '../lib/metrics-reset'
import type { ModelRouteMetrics, ModelRouteMetricsResponse } from '../types'

const OPTION_KEY =
  'model_route_setting.rate_limit_circuit_breaker_threshold' as const
const DEFAULT_THRESHOLD = 3
const MIN_THRESHOLD = 3
const MAX_THRESHOLD = 999

const defaultSettings = {
  [OPTION_KEY]: DEFAULT_THRESHOLD,
}

function useGlobalRateLimitCircuitBreakerThreshold() {
  const optionsQuery = useSystemOptions()
  const settings = useMemo(
    () => getOptionValue(optionsQuery.data?.data, defaultSettings),
    [optionsQuery.data?.data]
  )
  return {
    threshold: settings[OPTION_KEY],
    isLoading: optionsQuery.isLoading,
  }
}

export function CircuitBreakerSettings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { threshold: configuredThreshold, isLoading } =
    useGlobalRateLimitCircuitBreakerThreshold()
  const updateOption = useUpdateOption()
  const [threshold, setThreshold] = useState(String(configuredThreshold))

  useEffect(() => {
    setThreshold(String(configuredThreshold))
  }, [configuredThreshold])

  const parsedThreshold = Number(threshold.trim())
  const isValidThreshold =
    /^\d+$/.test(threshold.trim()) &&
    Number.isInteger(parsedThreshold) &&
    parsedThreshold >= MIN_THRESHOLD &&
    parsedThreshold <= MAX_THRESHOLD

  const handleSave = () => {
    if (!isValidThreshold) return
    updateOption.mutate(
      {
        key: OPTION_KEY,
        value: parsedThreshold,
      },
      {
        onSuccess: (res) => {
          if (res.success) {
            void queryClient.invalidateQueries({
              queryKey: ['model-route-metrics'],
            })
          }
        },
      }
    )
  }

  return (
    <section className='bg-muted/20 flex flex-col gap-3 rounded-lg border p-4'>
      <div className='flex flex-col gap-1'>
        <h2 className='text-sm font-semibold'>
          {t('Rate-limit circuit breaker')}
        </h2>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Configure how many consecutive 429 responses open a channel-model route.'
          )}
        </p>
      </div>
      <div className='flex flex-wrap items-end gap-3'>
        <div className='flex flex-col gap-1.5'>
          <label
            htmlFor='rate-limit-circuit-breaker-threshold'
            className='text-sm font-medium'
          >
            {t('429 failures before opening')}
          </label>
          <Input
            id='rate-limit-circuit-breaker-threshold'
            className='h-8 w-28'
            type='number'
            min={MIN_THRESHOLD}
            max={MAX_THRESHOLD}
            step='1'
            value={threshold}
            aria-invalid={!isValidThreshold}
            onChange={(event) => setThreshold(event.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {isValidThreshold
              ? t('Allowed range: 3-999')
              : t('Enter an integer from 3 to 999')}
          </p>
        </div>
        <Button
          size='sm'
          className='h-8'
          disabled={isLoading || updateOption.isPending || !isValidThreshold}
          onClick={handleSave}
        >
          {t('Save')}
        </Button>
      </div>
    </section>
  )
}

const isValidRouteThreshold = (value: string) => {
  const parsed = Number(value.trim())
  return (
    /^\d+$/.test(value.trim()) &&
    Number.isInteger(parsed) &&
    parsed >= MIN_THRESHOLD &&
    parsed <= MAX_THRESHOLD
  )
}

export function RateLimitThresholdEditor({ row }: { row: ModelRouteMetrics }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { threshold: globalThreshold, isLoading: globalLoading } =
    useGlobalRateLimitCircuitBreakerThreshold()
  const override = row.rate_limit_circuit_breaker_threshold
  const [value, setValue] = useState(override == null ? '' : String(override))

  useEffect(() => {
    setValue(override == null ? '' : String(override))
  }, [override])

  const thresholdMutation = useMutation({
    mutationFn: updateRateLimitCircuitBreakerThreshold,
    onSuccess: (res, variables) => {
      if (!res.success) {
        toast.error(res.message || t('Save failed'))
        return
      }
      queryClient.setQueryData<ModelRouteMetricsResponse>(
        ['model-route-metrics'],
        (current) =>
          patchMetricsRateLimitThreshold(
            current,
            variables,
            variables.threshold
          )
      )
      toast.success(t('Saved successfully'))
      void queryClient.invalidateQueries({ queryKey: ['model-route-metrics'] })
    },
    onError: (error: Error) => toast.error(error.message || t('Save failed')),
  })

  const trimmed = value.trim()
  const canClear = override != null
  const isValid = trimmed === '' || isValidRouteThreshold(trimmed)
  const save = () => {
    if (!isValid || thresholdMutation.isPending) return
    thresholdMutation.mutate({
      channel_id: row.channel_id,
      effective_model: row.effective_model,
      threshold: trimmed === '' ? null : Number(trimmed),
    })
  }

  return (
    <div className='flex min-w-[170px] flex-col gap-1.5'>
      <div className='flex items-center gap-1.5'>
        <span className='text-xs font-medium'>
          {t('429 threshold override')}
        </span>
        <Badge variant={canClear ? 'outline' : 'secondary'}>
          {canClear ? t('Override') : t('Inherited')}
        </Badge>
      </div>
      <div className='flex items-center gap-1.5'>
        <Input
          className='h-8 w-20'
          type='number'
          min={MIN_THRESHOLD}
          max={MAX_THRESHOLD}
          step='1'
          value={value}
          placeholder={String(globalThreshold)}
          aria-label={t('429 threshold override')}
          aria-invalid={!isValid}
          onChange={(event) => setValue(event.target.value)}
        />
        <Button
          size='sm'
          className='h-8'
          disabled={globalLoading || thresholdMutation.isPending || !isValid}
          onClick={save}
        >
          {t('Save')}
        </Button>
        {canClear && (
          <Button
            size='sm'
            variant='ghost'
            className='h-8 px-2'
            disabled={globalLoading || thresholdMutation.isPending}
            onClick={() =>
              thresholdMutation.mutate({
                channel_id: row.channel_id,
                effective_model: row.effective_model,
                threshold: null,
              })
            }
          >
            {t('Clear')}
          </Button>
        )}
      </div>
      <span className='text-muted-foreground text-[10px]'>
        {t('Global: {{value}}', {
          value: globalThreshold,
        })}
      </span>
      {!isValid && (
        <span className='text-destructive text-[10px]'>
          {t('Enter an integer from 3 to 999')}
        </span>
      )}
    </div>
  )
}
