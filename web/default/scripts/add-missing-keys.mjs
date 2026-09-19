import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    'Allowed range: 3-2147483647': 'Allowed range: 3-2147483647',
    'Enter an integer from 3 to 2147483647': 'Enter an integer from 3 to 2147483647',
  },
  zh: {
    'Allowed range: 3-2147483647': '允许范围：3-2147483647',
    'Enter an integer from 3 to 2147483647': '请输入 3 到 2147483647 之间的整数',
  },
  fr: {
    'Allowed range: 3-2147483647': 'Plage autorisée : 3-2147483647',
    'Enter an integer from 3 to 2147483647': 'Entrez un entier de 3 à 2147483647',
  },
  ja: {
    'Allowed range: 3-2147483647': '許可範囲: 3-2147483647',
    'Enter an integer from 3 to 2147483647': '3〜2147483647 の整数を入力してください',
  },
  ru: {
    'Allowed range: 3-2147483647': 'Допустимый диапазон: 3–2147483647',
    'Enter an integer from 3 to 2147483647': 'Введите целое число от 3 до 2147483647',
  },
  vi: {
    'Allowed range: 3-2147483647': 'Phạm vi cho phép: 3-2147483647',
    'Enter an integer from 3 to 2147483647': 'Nhập số nguyên từ 3 đến 2147483647',
  },
  'zh-TW': {
    'Allowed range: 3-2147483647': '允許範圍：3-2147483647',
    'Enter an integer from 3 to 2147483647': '請輸入 3 到 2147483647 之間的整數',
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
