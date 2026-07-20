import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    '{{available}}/{{count}} available': '{{available}}/{{count}} available',
    '{{count}} channel(s)': '{{count}} channel(s)',
    'All channels': 'All channels',
    'Available channels': 'Available channels',
    'Channel automatically disabled': 'Channel automatically disabled',
    'Channel manually disabled': 'Channel manually disabled',
    'Channel not found': 'Channel not found',
    'Channel unavailable': 'Channel unavailable',
    'Confirm reset to unknown for {{count}} selected metrics?':
      'Confirm reset to unknown for {{count}} selected metrics?',
    'Confirm reset to unknown': 'Confirm reset to unknown',
    'Failed to load enabled channels': 'Failed to load enabled channels',
    'Failed to reset state to unknown': 'Failed to reset state to unknown',
    'Limit this API key to selected enabled channels':
      'Limit this API key to selected enabled channels',
    'No enabled channels': 'No enabled channels',
    'No enabled channels found': 'No enabled channels found',
    'Only route requests through selected channels':
      'Only route requests through selected channels',
    'Policy disabled': 'Policy disabled',
    'Policy enabled': 'Policy enabled',
    'Policy status': 'Policy status',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.',
    'Reset to unknown': 'Reset to unknown',
    'Search channels...': 'Search channels...',
    'Select at least one channel': 'Select at least one channel',
    'Select channel #{{id}}': 'Select channel #{{id}}',
    'Specific channels': 'Specific channels',
    'State reset to unknown': 'State reset to unknown',
    'Use existing model-level routing without channel restrictions':
      'Use existing model-level routing without channel restrictions',
  },
  zh: {
    '{{available}}/{{count}} available': '{{available}}/{{count}} 可用',
    '{{count}} channel(s)': '{{count}} 个渠道',
    'All channels': '全部渠道',
    'Available channels': '可用渠道',
    'Channel automatically disabled': '渠道已自动禁用',
    'Channel manually disabled': '渠道已手动禁用',
    'Channel not found': '渠道不存在',
    'Channel unavailable': '渠道不可用',
    'Confirm reset to unknown for {{count}} selected metrics?':
      '确认将选中的 {{count}} 条指标重置为未知？',
    'Confirm reset to unknown': '确认重置为未知',
    'Failed to load enabled channels': '加载启用渠道失败',
    'Failed to reset state to unknown': '状态重置为未知失败',
    'Limit this API key to selected enabled channels':
      '限制此 API 密钥仅使用选中的启用渠道',
    'No enabled channels': '当前没有启用渠道',
    'No enabled channels found': '未找到启用渠道',
    'Only route requests through selected channels': '仅通过所选渠道路由请求',
    'Policy disabled': '策略停用',
    'Policy enabled': '策略启用',
    'Policy status': '策略状态',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      '将渠道 {{channel}} / 模型 {{model}} 重置为未知？此操作会清除退避状态，并在满足其他路由条件时使其重新进入生产池。如果上游仍然失败，状态机可能会立即再次将其置为开路状态。',
    'Reset to unknown': '重置为未知',
    'Search channels...': '搜索渠道...',
    'Select at least one channel': '至少选择一个渠道',
    'Select channel #{{id}}': '选择渠道 #{{id}}',
    'Specific channels': '指定渠道',
    'State reset to unknown': '状态已重置为未知',
    'Use existing model-level routing without channel restrictions':
      '使用现有模型级路由，不限制渠道',
  },
  fr: {
    '{{available}}/{{count}} available': '{{available}}/{{count}} disponibles',
    '{{count}} channel(s)': '{{count}} canaux',
    'All channels': 'Tous les canaux',
    'Available channels': 'Canaux disponibles',
    'Channel automatically disabled': 'Canal désactivé automatiquement',
    'Channel manually disabled': 'Canal désactivé manuellement',
    'Channel not found': 'Canal introuvable',
    'Channel unavailable': 'Canal indisponible',
    'Confirm reset to unknown for {{count}} selected metrics?':
      'Confirmer la réinitialisation à l’état inconnu de {{count}} métriques sélectionnées ?',
    'Confirm reset to unknown':
      'Confirmer la réinitialisation à l’état inconnu',
    'Failed to load enabled channels': 'Échec du chargement des canaux actifs',
    'Failed to reset state to unknown':
      'Échec de la réinitialisation à l’état inconnu',
    'Limit this API key to selected enabled channels':
      'Limiter cette clé API aux canaux actifs sélectionnés',
    'No enabled channels': 'Aucun canal actif',
    'No enabled channels found': 'Aucun canal actif trouvé',
    'Only route requests through selected channels':
      'Acheminer les requêtes uniquement via les canaux sélectionnés',
    'Policy disabled': 'Stratégie désactivée',
    'Policy enabled': 'Stratégie activée',
    'Policy status': 'État de la stratégie',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      'Réinitialiser le canal {{channel}} / modèle {{model}} à l’état inconnu ? Cela efface le délai d’attente et lui permet de réintégrer le pool de production si les autres conditions de routage sont remplies. Si le service en amont échoue encore, la machine à états peut le rouvrir immédiatement.',
    'Reset to unknown': 'Réinitialiser à l’état inconnu',
    'Search channels...': 'Rechercher des canaux...',
    'Select at least one channel': 'Sélectionnez au moins un canal',
    'Select channel #{{id}}': 'Sélectionner le canal n° {{id}}',
    'Specific channels': 'Canaux spécifiques',
    'State reset to unknown': 'État réinitialisé à l’état inconnu',
    'Use existing model-level routing without channel restrictions':
      'Utiliser le routage par modèle existant sans limiter les canaux',
  },
  ja: {
    '{{available}}/{{count}} available': '{{available}}/{{count}} 利用可能',
    '{{count}} channel(s)': '{{count}} チャネル',
    'All channels': 'すべてのチャネル',
    'Available channels': '利用可能なチャネル',
    'Channel automatically disabled': 'チャネルは自動無効化済み',
    'Channel manually disabled': 'チャネルは手動無効化済み',
    'Channel not found': 'チャネルが見つかりません',
    'Channel unavailable': 'チャネルは利用不可',
    'Confirm reset to unknown for {{count}} selected metrics?':
      '選択した {{count}} 件のメトリクスを不明状態にリセットしますか？',
    'Confirm reset to unknown': '不明状態へのリセットを確認',
    'Failed to load enabled channels': '有効なチャネルを読み込めませんでした',
    'Failed to reset state to unknown': '状態を不明にリセットできませんでした',
    'Limit this API key to selected enabled channels':
      'この API キーを選択した有効なチャネルに限定',
    'No enabled channels': '有効なチャネルなし',
    'No enabled channels found': '有効なチャネルが見つかりません',
    'Only route requests through selected channels':
      '選択したチャネルのみでリクエストをルーティング',
    'Policy disabled': 'ポリシー無効',
    'Policy enabled': 'ポリシー有効',
    'Policy status': 'ポリシー状態',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      'チャネル {{channel}} / モデル {{model}} を不明状態にリセットしますか？バックオフを解除し、他のルーティング条件を満たす場合は本番プールに復帰させます。アップストリームで引き続き失敗する場合、ステートマシンによって直ちに再びオープン状態になることがあります。',
    'Reset to unknown': '不明状態にリセット',
    'Search channels...': 'チャネルを検索...',
    'Select at least one channel': '1つ以上のチャネルを選択してください',
    'Select channel #{{id}}': 'チャネル #{{id}} を選択',
    'Specific channels': '指定チャネル',
    'State reset to unknown': '状態を不明にリセットしました',
    'Use existing model-level routing without channel restrictions':
      'チャネルを制限せず既存のモデル単位ルーティングを使用',
  },
  ru: {
    '{{available}}/{{count}} available': '{{available}}/{{count}} доступно',
    '{{count}} channel(s)': 'Каналов: {{count}}',
    'All channels': 'Все каналы',
    'Available channels': 'Доступные каналы',
    'Channel automatically disabled': 'Канал отключён автоматически',
    'Channel manually disabled': 'Канал отключён вручную',
    'Channel not found': 'Канал не найден',
    'Channel unavailable': 'Канал недоступен',
    'Confirm reset to unknown for {{count}} selected metrics?':
      'Сбросить выбранные метрики ({{count}}) в неизвестное состояние?',
    'Confirm reset to unknown': 'Подтвердить сброс в неизвестное состояние',
    'Failed to load enabled channels': 'Не удалось загрузить включённые каналы',
    'Failed to reset state to unknown':
      'Не удалось сбросить состояние в неизвестное',
    'Limit this API key to selected enabled channels':
      'Ограничить этот API-ключ выбранными включёнными каналами',
    'No enabled channels': 'Нет включённых каналов',
    'No enabled channels found': 'Включённые каналы не найдены',
    'Only route requests through selected channels':
      'Направлять запросы только через выбранные каналы',
    'Policy disabled': 'Политика отключена',
    'Policy enabled': 'Политика включена',
    'Policy status': 'Статус политики',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      'Сбросить канал {{channel}} / модель {{model}} в неизвестное состояние? Это очистит задержку повтора и позволит маршруту вернуться в рабочий пул при выполнении остальных условий маршрутизации. Если вышестоящий сервис продолжит отвечать с ошибкой, автомат состояний может сразу снова разомкнуть маршрут.',
    'Reset to unknown': 'Сбросить в неизвестное состояние',
    'Search channels...': 'Поиск каналов...',
    'Select at least one channel': 'Выберите хотя бы один канал',
    'Select channel #{{id}}': 'Выбрать канал № {{id}}',
    'Specific channels': 'Выбранные каналы',
    'State reset to unknown': 'Состояние сброшено в неизвестное',
    'Use existing model-level routing without channel restrictions':
      'Использовать текущую маршрутизацию по моделям без ограничений каналов',
  },
  vi: {
    '{{available}}/{{count}} available': '{{available}}/{{count}} khả dụng',
    '{{count}} channel(s)': '{{count}} kênh',
    'All channels': 'Tất cả kênh',
    'Available channels': 'Kênh khả dụng',
    'Channel automatically disabled': 'Kênh đã tự động tắt',
    'Channel manually disabled': 'Kênh đã tắt thủ công',
    'Channel not found': 'Không tìm thấy kênh',
    'Channel unavailable': 'Kênh không khả dụng',
    'Confirm reset to unknown for {{count}} selected metrics?':
      'Xác nhận đặt lại {{count}} chỉ số đã chọn về trạng thái chưa xác định?',
    'Confirm reset to unknown': 'Xác nhận đặt lại về trạng thái chưa xác định',
    'Failed to load enabled channels': 'Không thể tải các kênh đã bật',
    'Failed to reset state to unknown':
      'Không thể đặt lại về trạng thái chưa xác định',
    'Limit this API key to selected enabled channels':
      'Giới hạn khóa API này ở các kênh đã bật được chọn',
    'No enabled channels': 'Không có kênh đã bật',
    'No enabled channels found': 'Không tìm thấy kênh đã bật',
    'Only route requests through selected channels':
      'Chỉ định tuyến yêu cầu qua các kênh đã chọn',
    'Policy disabled': 'Chính sách đã tắt',
    'Policy enabled': 'Chính sách đã bật',
    'Policy status': 'Trạng thái chính sách',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      'Đặt lại kênh {{channel}} / mô hình {{model}} về trạng thái chưa xác định? Thao tác này xóa thời gian chờ lùi và cho phép tuyến quay lại nhóm phục vụ khi đáp ứng các điều kiện định tuyến khác. Nếu thượng nguồn vẫn lỗi, máy trạng thái có thể lập tức mở mạch lại.',
    'Reset to unknown': 'Đặt lại về trạng thái chưa xác định',
    'Search channels...': 'Tìm kiếm kênh...',
    'Select at least one channel': 'Chọn ít nhất một kênh',
    'Select channel #{{id}}': 'Chọn kênh #{{id}}',
    'Specific channels': 'Kênh chỉ định',
    'State reset to unknown': 'Đã đặt lại về trạng thái chưa xác định',
    'Use existing model-level routing without channel restrictions':
      'Dùng định tuyến cấp mô hình hiện có, không giới hạn kênh',
  },
  'zh-TW': {
    '{{available}}/{{count}} available': '{{available}}/{{count}} 可用',
    '{{count}} channel(s)': '{{count}} 個渠道',
    'All channels': '全部渠道',
    'Available channels': '可用渠道',
    'Channel automatically disabled': '渠道已自動停用',
    'Channel manually disabled': '渠道已手動停用',
    'Channel not found': '渠道不存在',
    'Channel unavailable': '渠道不可用',
    'Confirm reset to unknown for {{count}} selected metrics?':
      '確認將選取的 {{count}} 筆指標重設為未知？',
    'Confirm reset to unknown': '確認重設為未知',
    'Failed to load enabled channels': '載入啟用渠道失敗',
    'Failed to reset state to unknown': '狀態重設為未知失敗',
    'Limit this API key to selected enabled channels':
      '限制此 API 金鑰僅使用選中的啟用渠道',
    'No enabled channels': '目前沒有啟用渠道',
    'No enabled channels found': '找不到啟用渠道',
    'Only route requests through selected channels': '僅透過所選渠道路由請求',
    'Policy disabled': '策略停用',
    'Policy enabled': '策略啟用',
    'Policy status': '策略狀態',
    'Reset channel {{channel}} / model {{model}} to unknown? This clears backoff and lets it re-enter the production pool when other routing conditions are met. If the upstream still fails, the state machine may open it again immediately.':
      '將渠道 {{channel}} / 模型 {{model}} 重設為未知？此操作會清除退避狀態，並在符合其他路由條件時讓它重新進入生產集區。如果上游仍失敗，狀態機可能會立即再次將它設為開路狀態。',
    'Reset to unknown': '重設為未知',
    'Search channels...': '搜尋渠道...',
    'Select at least one channel': '至少選擇一個渠道',
    'Select channel #{{id}}': '選擇渠道 #{{id}}',
    'Specific channels': '指定渠道',
    'State reset to unknown': '狀態已重設為未知',
    'Use existing model-level routing without channel restrictions':
      '使用現有模型級路由，不限制渠道',
  },
}

async function main() {
  let totalApplied = 0

  for (const [locale, trans] of Object.entries(newKeys)) {
    const filePath = path.join(LOCALES_DIR, `${locale}.json`)
    const json = JSON.parse(await fs.readFile(filePath, 'utf8'))

    let count = 0
    for (const [key, value] of Object.entries(trans)) {
      if (!Object.prototype.hasOwnProperty.call(json.translation, key)) {
        json.translation[key] = value
        count++
      } else if (json.translation[key] !== value) {
        json.translation[key] = value
        count++
      }
    }

    if (count > 0) {
      json.translation = Object.fromEntries(
        Object.entries(json.translation).sort(([a], [b]) => a.localeCompare(b))
      )
      await fs.writeFile(filePath, stableStringify(json), 'utf8')
    }

    console.log(`${locale}: ${count} translations applied`)
    totalApplied += count
  }

  console.log(`\nTotal: ${totalApplied} translations applied`)
}

main().catch((err) => {
  console.error(err)
  process.exitCode = 1
})
