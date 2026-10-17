import { ref } from 'vue'
import { api, type Role, type User } from './api'

// The signed-in user; undefined until loaded.
export const user = ref<User>()

const rank: Record<Role, number> = { viewer: 1, editor: 2, admin: 3 }

/** Whether the current user has at least `role`. The API checks again. */
export function can(role: Role): boolean {
  return !!user.value && rank[user.value.role] >= rank[role]
}

export async function loadUser(): Promise<User | undefined> {
  try {
    user.value = await api.me()
  } catch {
    user.value = undefined
  }
  return user.value
}

export async function logout() {
  try {
    await api.logout()
  } finally {
    user.value = undefined
    location.assign('/login')
  }
}
