import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    "Global rate limit": "Global rate limit",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).",
    "Window (seconds)": "Window (seconds)",
    "Max requests": "Max requests",
    "Rate limit override": "Rate limit override",
    "Inherit global: {{window}}s / {{count}} req": "Inherit global: {{window}}s / {{count}} req",
    "Inherit global: Unlimited": "Inherit global: Unlimited",
    "req": "req",
    'Allowed range: 3-2147483647': 'Allowed range: 3-2147483647',
    'Enter an integer from 3 to 2147483647': 'Enter an integer from 3 to 2147483647',
  },
  zh: {
    "Global rate limit": "全局频次限制",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "配置所有渠道-模型路由默认继承的时间窗口与最大请求频次（0 表示不限）。",
    "Window (seconds)": "时间窗口 (秒)",
    "Max requests": "最大请求数",
    "Rate limit override": "频次限制单独覆盖",
    "Inherit global: {{window}}s / {{count}} req": "全局: 每 {{window}}秒 {{count}}次",
    "Inherit global: Unlimited": "全局: 不限制",
    "req": "次",
    'Allowed range: 3-2147483647': '允许范围：3-2147483647',
    'Enter an integer from 3 to 2147483647': '请输入 3 到 2147483647 之间的整数',
  },
  'zh-TW': {
    "Global rate limit": "全域頻次限制",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "設定所有渠道-模型路由預設繼承的時間窗口與最大請求頻次（0 表示不限）。",
    "Window (seconds)": "時間窗口 (秒)",
    "Max requests": "最大請求數",
    "Rate limit override": "頻次限制單獨覆蓋",
    "Inherit global: {{window}}s / {{count}} req": "全域: 每 {{window}}秒 {{count}}次",
    "Inherit global: Unlimited": "全域: 不限制",
    "req": "次",
    'Allowed range: 3-2147483647': '允許範圍：3-2147483647',
    'Enter an integer from 3 to 2147483647': '請輸入 3 到 2147483647 之間的整數',
  },
  ja: {
    "Global rate limit": "グローバルレート制限",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "すべてのチャネル・モデルルートがデフォルトで継承する時間枠と最大リクエスト数を設定します（0は無制限）。",
    "Window (seconds)": "ウィンドウ (秒)",
    "Max requests": "最大リクエスト数",
    "Rate limit override": "レート制限の個別上書き",
    "Inherit global: {{window}}s / {{count}} req": "グローバル: {{window}}秒ごとに{{count}}回",
    "Inherit global: Unlimited": "グローバル: 無制限",
    "req": "回",
    'Allowed range: 3-2147483647': '許可範囲: 3-2147483647',
    'Enter an integer from 3 to 2147483647': '3〜2147483647 の整数を入力してください',
  },
  ru: {
    "Global rate limit": "Глобальный лимит частоты",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "Настройка окна времени и макс. числа запросов по умолчанию для всех маршрутов (0 = без ограничений).",
    "Window (seconds)": "Окно (секунд)",
    "Max requests": "Макс. запросов",
    "Rate limit override": "Индивидуальное ограничение",
    "Inherit global: {{window}}s / {{count}} req": "Глобальный: каждые {{window}}с {{count}} запр.",
    "Inherit global: Unlimited": "Глобальный: Без ограничений",
    "req": "запр.",
    'Allowed range: 3-2147483647': 'Допустимый диапазон: 3–2147483647',
    'Enter an integer from 3 to 2147483647': 'Введите целое число от 3 до 2147483647',
  },
  vi: {
    "Global rate limit": "Giới hạn tần suất toàn cục",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "Cấu hình cửa sổ thời gian và số yêu cầu tối đa mặc định cho tất cả tuyến kênh-mô hình (0 = không giới hạn).",
    "Window (seconds)": "Cửa sổ (giây)",
    "Max requests": "Số yêu cầu tối đa",
    "Rate limit override": "Ghi đè giới hạn tần suất",
    "Inherit global: {{window}}s / {{count}} req": "Toàn cục: mỗi {{window}}s {{count}} yêu cầu",
    "Inherit global: Unlimited": "Toàn cục: Không giới hạn",
    "req": "y/c",
    'Allowed range: 3-2147483647': 'Phạm vi cho phép: 3-2147483647',
    'Enter an integer from 3 to 2147483647': 'Nhập số nguyên từ 3 đến 2147483647',
  },
  fr: {
    "Global rate limit": "Limite de débit globale",
    "Configure the default rate limit inherited by all channel-model routes (0 = unlimited).": "Configurer la fenêtre de temps et le nombre max de requêtes par défaut pour toutes les routes (0 = illimité).",
    "Window (seconds)": "Fenêtre (secondes)",
    "Max requests": "Requêtes max",
    "Rate limit override": "Remplacement de limite",
    "Inherit global: {{window}}s / {{count}} req": "Global : toutes les {{window}}s {{count}} req",
    "Inherit global: Unlimited": "Global : Illimité",
    "req": "req",
    'Allowed range: 3-2147483647': 'Plage autorisée : 3-2147483647',
    'Enter an integer from 3 to 2147483647': 'Entrez un entier de 3 à 2147483647',
  }
}

async function main() {
  let totalAdded = 0

  for (const [locale, trans] of Object.entries(newKeys)) {
    const filePath = path.join(LOCALES_DIR, `${locale}.json`)
    let json
    try {
      json = JSON.parse(await fs.readFile(filePath, 'utf8'))
    } catch {
      continue
    }

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
    totalAdded += count
  }

  console.log(`\nTotal: ${totalAdded} translations applied`)
}

main().catch((err) => { console.error(err); process.exitCode = 1 })
