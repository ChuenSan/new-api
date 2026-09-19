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

import { updateRateLimitCircuitBreakerThreshold, updatePreflightRateLimit } from '../api'
import { patchMetricsRateLimitThreshold, patchMetricsPreflightRateLimit } from '../lib/metrics-reset'
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

const RL_WINDOW_KEY = 'model_route_setting.rate_limit_window_seconds' as const
const RL_MAX_KEY = 'model_route_setting.rate_limit_max_requests' as const

const defaultRateLimitSettings = {
  [RL_WINDOW_KEY]: 0,
  [RL_MAX_KEY]: 0,
}

function useGlobalPreflightRateLimit() {
  const optionsQuery = useSystemOptions()
  const settings = useMemo(
    () => getOptionValue(optionsQuery.data?.data, defaultRateLimitSettings),
    [optionsQuery.data?.data]
  )
  return {
    window: settings[RL_WINDOW_KEY],
    max: settings[RL_MAX_KEY],
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

export function GlobalRateLimitSettings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { window: configuredWindow, max: configuredMax, isLoading } =
    useGlobalPreflightRateLimit()
  const updateOption = useUpdateOption()
  const [windowVal, setWindowVal] = useState(String(configuredWindow))
  const [maxVal, setMaxVal] = useState(String(configuredMax))

  useEffect(() => {
    setWindowVal(String(configuredWindow))
  }, [configuredWindow])
  useEffect(() => {
    setMaxVal(String(configuredMax))
  }, [configuredMax])

  const parsedWindow = Number(windowVal.trim())
  const parsedMax = Number(maxVal.trim())
  const isValidWindow =
    /^\d+$/.test(windowVal.trim()) &&
    Number.isInteger(parsedWindow) &&
    parsedWindow >= 0 &&
    parsedWindow <= 86400
  const isValidMax =
    /^\d+$/.test(maxVal.trim()) &&
    Number.isInteger(parsedMax) &&
    parsedMax >= 0 &&
    parsedMax <= 1000000
  const isValid = isValidWindow && isValidMax

  const handleSave = () => {
    if (!isValid) return
    // Save both sequentially
    updateOption.mutate(
      { key: RL_WINDOW_KEY, value: parsedWindow },
      {
        onSuccess: (res) => {
          if (!res.success) return
          updateOption.mutate(
            { key: RL_MAX_KEY, value: parsedMax },
            {
              onSuccess: (res2) => {
                if (res2.success) {
                  void queryClient.invalidateQueries({
                    queryKey: ['model-route-metrics'],
                  })
                }
              },
            }
          )
        },
      }
    )
  }

  return (
    <section className='bg-muted/20 flex flex-col gap-3 rounded-lg border p-4'>
      <div className='flex flex-col gap-1'>
        <h2 className='text-sm font-semibold'>
          {t('Global rate limit')}
        </h2>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Configure the default rate limit inherited by all channel-model routes (0 = unlimited).'
          )}
        </p>
      </div>
      <div className='flex flex-wrap items-end gap-3'>
        <div className='flex flex-col gap-1.5'>
          <label
            htmlFor='rate-limit-window-seconds'
            className='text-sm font-medium'
          >
            {t('Window (seconds)')}
          </label>
          <Input
            id='rate-limit-window-seconds'
            className='h-8 w-28'
            type='number'
            min={0}
            max={86400}
            step='1'
            value={windowVal}
            aria-invalid={!isValidWindow}
            onChange={(event) => setWindowVal(event.target.value)}
          />
        </div>
        <div className='flex flex-col gap-1.5'>
          <label
            htmlFor='rate-limit-max-requests'
            className='text-sm font-medium'
          >
            {t('Max requests')}
          </label>
          <Input
            id='rate-limit-max-requests'
            className='h-8 w-28'
            type='number'
            min={0}
            max={1000000}
            step='1'
            value={maxVal}
            aria-invalid={!isValidMax}
            onChange={(event) => setMaxVal(event.target.value)}
          />
        </div>
        <Button
          size='sm'
          className='h-8'
          disabled={isLoading || updateOption.isPending || !isValid}
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

export function PreflightRateLimitEditor({ row }: { row: ModelRouteMetrics }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { window: globalWindow, max: globalMax, isLoading: globalLoading } =
    useGlobalPreflightRateLimit()
  const overrideWindow = row.rate_limit_window_seconds
  const overrideMax = row.rate_limit_max_requests
  const hasOverride = overrideWindow != null && overrideMax != null
  const [windowVal, setWindowVal] = useState(
    overrideWindow == null ? '' : String(overrideWindow)
  )
  const [maxVal, setMaxVal] = useState(
    overrideMax == null ? '' : String(overrideMax)
  )

  useEffect(() => {
    setWindowVal(overrideWindow == null ? '' : String(overrideWindow))
  }, [overrideWindow])
  useEffect(() => {
    setMaxVal(overrideMax == null ? '' : String(overrideMax))
  }, [overrideMax])

  const mutation = useMutation({
    mutationFn: updatePreflightRateLimit,
    onSuccess: (res, variables) => {
      if (!res.success) {
        toast.error(res.message || t('Save failed'))
        return
      }
      queryClient.setQueryData<ModelRouteMetricsResponse>(
        ['model-route-metrics'],
        (current) =>
          patchMetricsPreflightRateLimit(
            current,
            variables,
            variables.window_seconds,
            variables.max_requests
          )
      )
      toast.success(t('Saved successfully'))
      void queryClient.invalidateQueries({ queryKey: ['model-route-metrics'] })
    },
    onError: (error: Error) => toast.error(error.message || t('Save failed')),
  })

  const trimmedW = windowVal.trim()
  const trimmedM = maxVal.trim()
  const isValidW = trimmedW === '' || (/^\d+$/.test(trimmedW) && Number(trimmedW) >= 0)
  const isValidM = trimmedM === '' || (/^\d+$/.test(trimmedM) && Number(trimmedM) >= 0)
  const isValid = isValidW && isValidM

  const save = () => {
    if (!isValid || mutation.isPending) return
    mutation.mutate({
      channel_id: row.channel_id,
      effective_model: row.effective_model,
      window_seconds: trimmedW === '' ? null : Number(trimmedW),
      max_requests: trimmedM === '' ? null : Number(trimmedM),
    })
  }

  const clear = () => {
    if (mutation.isPending) return
    mutation.mutate({
      channel_id: row.channel_id,
      effective_model: row.effective_model,
      window_seconds: null,
      max_requests: null,
    })
  }

  const globalLabel =
    globalWindow > 0 && globalMax > 0
      ? t('Inherit global: {{window}}s / {{count}} req', {
          window: globalWindow,
          count: globalMax,
        })
      : t('Inherit global: Unlimited')

  return (
    <div className='flex min-w-[200px] flex-col gap-1.5'>
      <div className='flex items-center gap-1.5'>
        <span className='text-xs font-medium'>
          {t('Rate limit override')}
        </span>
        <Badge variant={hasOverride ? 'outline' : 'secondary'}>
          {hasOverride ? t('Override') : t('Inherited')}
        </Badge>
      </div>
      <div className='flex items-center gap-1.5'>
        <Input
          className='h-8 w-16'
          type='number'
          min={0}
          step='1'
          value={windowVal}
          placeholder='T'
          aria-label={t('Window (seconds)')}
          aria-invalid={!isValidW}
          onChange={(event) => setWindowVal(event.target.value)}
        />
        <span className='text-muted-foreground text-xs'>s</span>
        <Input
          className='h-8 w-16'
          type='number'
          min={0}
          step='1'
          value={maxVal}
          placeholder='N'
          aria-label={t('Max requests')}
          aria-invalid={!isValidM}
          onChange={(event) => setMaxVal(event.target.value)}
        />
        <span className='text-muted-foreground text-xs'>{t('req')}</span>
        <Button
          size='sm'
          className='h-8'
          disabled={globalLoading || mutation.isPending || !isValid}
          onClick={save}
        >
          {t('Save')}
        </Button>
        {hasOverride && (
          <Button
            size='sm'
            variant='ghost'
            className='h-8 px-2'
            disabled={globalLoading || mutation.isPending}
            onClick={clear}
          >
            {t('Clear')}
          </Button>
        )}
      </div>
      <span className='text-muted-foreground text-[10px]'>
        {globalLabel}
      </span>
    </div>
  )
}
