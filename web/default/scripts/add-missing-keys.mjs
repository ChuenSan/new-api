import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    'Runs automatically after saving': 'Runs automatically after saving',
  },
  zh: {
    'Runs automatically after saving': '保存后自动执行',
  },
  fr: {
    'Runs automatically after saving':
      "S'exécute automatiquement après l'enregistrement",
  },
  ja: {
    'Runs automatically after saving': '保存後に自動的に実行されます',
  },
  ru: {
    'Runs automatically after saving':
      'Автоматически выполняется после сохранения',
  },
  vi: {
    'Runs automatically after saving': 'Tự động chạy sau khi lưu',
  },
  'zh-TW': {
    'Runs automatically after saving': '儲存後自動執行',
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
