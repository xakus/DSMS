// i18n: JSON-словари, EN — базовый, RU — второй (разд. 6.1 ТЗ).
// Добавление нового языка = один JSON-файл + строка в messages.
import { createI18n } from 'vue-i18n'
import en from './en.json'
import ru from './ru.json'

export const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('dsms.locale') ?? 'en',
  fallbackLocale: 'en',
  messages: { en, ru },
})
