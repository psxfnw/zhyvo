export function normalizeSlug(value: string) {
  const match = value.toUpperCase().match(/(?:\/R\/)?([A-Z0-9]{6,12})(?:[/?#]|$)/)
  return (match?.[1] ?? value).replace(/[^A-Z0-9]/g, '').slice(0, 12)
}

export function bytes(value: number) {
  if (value < 1024) return `${value} Б`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(0)} КБ`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} МБ`
  return `${(value / 1024 ** 3).toFixed(1)} ГБ`
}

export function remaining(expiresAt: string) {
  const milliseconds = Math.max(0, new Date(expiresAt).getTime() - Date.now())
  const hours = Math.ceil(milliseconds / 3_600_000)
  if (hours <= 24) return { value: hours, unit: 'год', detail: 'до видалення' }
  return { value: Math.ceil(hours / 24), unit: 'дні', detail: 'до видалення' }
}

export function errorMessage(error: unknown) {
  if (!(error instanceof Error)) return 'Сталася невідома помилка'
  const known: Record<string, string> = {
    'PIN or password is incorrect': 'Неправильний PIN або пароль',
    'Room not found': 'Кімнату не знайдено',
    'Room has expired': 'Термін дії кімнати завершився',
    'File contents do not match the selected media type': 'Вміст файлу не відповідає вибраному формату фото або відео',
    'Room storage or file limit has been reached': 'У кімнаті закінчилось доступне місце або вичерпано ліміт файлів',
    'Too many requests; try again later': 'Забагато запитів. Спробуйте ще раз за хвилину',
  }
  return known[error.message] ?? error.message
}
