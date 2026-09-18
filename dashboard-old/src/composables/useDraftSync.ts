import { watch, type Ref } from 'vue'

/** Refetch updates pristine forms; edits remain local until explicitly saved. */
export function useDraftSync<T>(data: Readonly<Ref<T | null | undefined>>, snapshot: () => unknown, seed: (value: T) => void) {
  let baseline: string | undefined
  function accept(value: T): void { seed(value); baseline = JSON.stringify(snapshot()) }
  watch(data, value => {
    if (value && (baseline === undefined || JSON.stringify(snapshot()) === baseline)) accept(value)
  }, { immediate: true })
  return { accept }
}
