<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useDark, useToggle } from '@vueuse/core'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useI18n } from 'vue-i18n'
import { Globe, KeyRound, Mail, Moon, Sun } from 'lucide-vue-next'
import { loginWithEmail, storeAccessToken } from './api/auth'
import logoUrl from '@/assets/logo.png'

const { t, locale } = useI18n()
const router = useRouter()
const isDark = useDark({
  selector: 'html',
  attribute: 'class',
  valueDark: 'dark',
  valueLight: '',
})
const toggleDark = useToggle(isDark)
const toggleLocale = () => {
  locale.value = locale.value === 'zh-CN' ? 'en-US' : 'zh-CN'
}

const email = ref('')
const password = ref('')
const isLoading = ref(false)
const statusKey = ref<string | null>(null)
const errorKey = ref<string | null>(null)

type PasswordCredentialInit = {
  id: string
  password: string
  name?: string
}

type PasswordCredentialConstructor = new (data: PasswordCredentialInit) => Credential

/**
 * Vue handles the login request with fetch, so some browsers do not see a
 * native navigation they can use to offer saving the password.  Store the
 * credential when the browser exposes the API; the regular autocomplete
 * attributes below remain the cross-browser fallback.
 */
const storeBrowserCredential = async (username: string, secret: string): Promise<void> => {
  if (typeof window === 'undefined' || typeof navigator === 'undefined') return

  try {
    const credentialContainer = navigator.credentials
    const PasswordCredential = (window as Window & {
      PasswordCredential?: PasswordCredentialConstructor
    }).PasswordCredential
    if (!credentialContainer || !PasswordCredential) return

    const credential = new PasswordCredential({
      id: username,
      password: secret,
      name: username,
    })
    await credentialContainer.store(credential)
  } catch {
    // Credential storage is an optional browser enhancement. Never block login.
  }
}

const formValue = (formData: FormData, name: string, fallback: string): string => {
  const value = formData.get(name)
  return typeof value === 'string' ? value : fallback
}

const handleLogin = async (event: SubmitEvent) => {
  if (isLoading.value) return

  // Autofill implementations are allowed to update the native control without
  // dispatching an input event. Read the controls directly so the request uses
  // the values the user actually submitted.
  const form = event.currentTarget
  const formData = typeof HTMLFormElement !== 'undefined' && form instanceof HTMLFormElement
    ? new FormData(form)
    : new FormData()
  const submittedEmail = formValue(
    formData,
    'username',
    formValue(formData, 'email', email.value),
  ).trim()
  const submittedPassword = formValue(formData, 'password', password.value)

  email.value = submittedEmail
  password.value = submittedPassword
  isLoading.value = true
  statusKey.value = null
  errorKey.value = null

  try {
    const response = await loginWithEmail({
      email: submittedEmail,
      password: submittedPassword,
    })
    storeAccessToken(response.accessToken)
    // Do not delay navigation if a browser prompts, rejects, or lacks storage
    // support; the credential request has already been started at this point.
    void storeBrowserCredential(submittedEmail, submittedPassword)
    statusKey.value = 'auth.login.success'
    await router.push('/admin')
  } catch (error) {
    errorKey.value = error instanceof Error ? error.message : 'auth.errors.unknown'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center bg-background p-4 sm:p-6">
    <div class="w-full max-w-sm">
      <div class="relative overflow-hidden rounded-xl border border-border/70 border-t-2 border-t-primary bg-surface-elevated p-6 shadow-xl shadow-black/10 sm:p-7">
        <div class="mb-1 flex justify-end gap-1">
          <button
            type="button"
            class="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-surface-line hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            :title="t('admin.layout.toggleLanguage')"
            :aria-label="t('admin.layout.toggleLanguage')"
            @click="toggleLocale"
          >
            <Globe class="h-4 w-4" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-surface-line hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            :title="t('admin.layout.toggleTheme')"
            :aria-label="t('admin.layout.toggleTheme')"
            @click="toggleDark()"
          >
            <Moon v-if="!isDark" class="h-4 w-4" aria-hidden="true" />
            <Sun v-else class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <div class="mb-7 text-center">
          <img :src="logoUrl" :alt="t('brand.logoAlt')" width="48" height="48" class="mx-auto mb-4 h-12 w-12 object-contain" />
          <h1 class="text-balance text-2xl font-semibold text-foreground">{{ t('auth.login.title') }}</h1>
          <p class="text-sm text-muted-foreground mt-2">{{ t('auth.login.subtitle') }}</p>
        </div>

        <form
          id="login-form"
          name="login"
          method="post"
          action="/api/auth/login"
          autocomplete="on"
          @submit.prevent="handleLogin"
          class="space-y-5"
        >
          <div class="space-y-2">
            <label for="login-email" class="text-sm font-medium text-foreground">{{ t('auth.login.email') }}</label>
            <div class="relative">
              <Mail class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
              <Input
                id="login-email"
                v-model="email"
                name="username"
                type="email"
                :placeholder="t('auth.login.emailPlaceholder')" 
                class="pl-10 h-12 bg-surface border-border/50 focus:border-primary"
                autocomplete="username"
                spellcheck="false"
                required
              />
            </div>
          </div>

          <div class="space-y-2">
            <label for="login-password" class="text-sm font-medium text-foreground">{{ t('auth.login.password') }}</label>
            <div class="relative">
              <KeyRound class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
              <Input
                id="login-password"
                v-model="password"
                name="password"
                type="password" 
                :placeholder="t('auth.login.passwordPlaceholder')" 
                class="pl-10 h-12 bg-surface border-border/50 focus:border-primary"
                autocomplete="current-password"
                required
              />
            </div>
          </div>

          <p
            v-if="statusKey"
            class="rounded-lg border border-signal/20 bg-signal/10 px-4 py-3 text-sm font-medium text-signal"
            role="status"
            aria-live="polite"
          >
            {{ t(statusKey) }}
          </p>

          <p
            v-if="errorKey"
            class="rounded-lg border border-destructive/20 bg-destructive/10 px-4 py-3 text-sm font-medium text-destructive"
            role="alert"
          >
            {{ t(errorKey) }}
          </p>

          <Button 
            type="submit" 
            class="mt-2 h-12 w-full text-base font-semibold"
            :disabled="isLoading"
          >
            {{ isLoading ? t('auth.login.submitting') : t('auth.login.submit') }}
          </Button>
        </form>
      </div>
    </div>
  </main>
</template>
