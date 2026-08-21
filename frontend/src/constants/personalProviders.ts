import type { AccountPlatform } from '@/types'

const baseButtonClass =
  'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all'
const inactiveButtonClass =
  `${baseButtonClass} text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200`

/** Providers exposed by the Personal Edition account setup UI. */
export const PERSONAL_ACCOUNT_PROVIDERS: ReadonlyArray<{
  id: Extract<AccountPlatform, 'openai' | 'gemini' | 'anthropic'>
  label: string
  activeClass: string
  inactiveClass: string
}> = [
  {
    id: 'openai',
    label: 'OpenAI',
    activeClass: `${baseButtonClass} bg-white text-green-600 shadow-sm dark:bg-dark-600 dark:text-green-400`,
    inactiveClass: inactiveButtonClass
  },
  {
    id: 'gemini',
    label: 'Gemini',
    activeClass: `${baseButtonClass} bg-white text-blue-600 shadow-sm dark:bg-dark-600 dark:text-blue-400`,
    inactiveClass: inactiveButtonClass
  },
  {
    id: 'anthropic',
    label: 'Anthropic / Claude',
    activeClass: `${baseButtonClass} bg-white text-orange-600 shadow-sm dark:bg-dark-600 dark:text-orange-400`,
    inactiveClass: inactiveButtonClass
  }
]

export const PERSONAL_ACCOUNT_PLATFORM_IDS = new Set<AccountPlatform>(
  PERSONAL_ACCOUNT_PROVIDERS.map((provider) => provider.id)
)
