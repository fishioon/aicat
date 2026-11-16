import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import type { Ref, WatchSource } from "vue";

export function useFollowLatest(
  viewport: Ref<HTMLElement | undefined>,
  content: WatchSource<unknown>,
  positionChanged: () => void = () => {},
) {
  const following = ref(true);
  let observer: ResizeObserver | undefined;
  let height = 0;
  let contentHeight = 0;
  let expectedTop: number | undefined;

  function geometry() {
    height = viewport.value?.clientHeight ?? 0;
    contentHeight = viewport.value?.scrollHeight ?? 0;
  }

  function position(top: number) {
    const element = viewport.value;
    if (!element) return;
    element.scrollTop = top;
    expectedTop = element.scrollTop;
    geometry();
    positionChanged();
  }
  function latest() {
    position(viewport.value?.scrollHeight ?? 0);
  }
  function resetPosition() {
    position(0);
  }
  function scroll() {
    const element = viewport.value;
    if (!element) return;
    if (expectedTop !== undefined) {
      const expected = Math.abs(element.scrollTop - expectedTop) <= 1;
      expectedTop = undefined;
      if (expected) {
        geometry();
        positionChanged();
        return;
      }
    }
    if (
      element.clientHeight !== height ||
      element.scrollHeight !== contentHeight
    ) {
      if (following.value) latest();
      else {
        geometry();
        positionChanged();
      }
      return;
    }
    following.value =
      element.scrollHeight - element.clientHeight - element.scrollTop <= 8;
    positionChanged();
  }
  function toggle(event: Event) {
    following.value = (event.target as HTMLInputElement).checked;
    if (following.value) latest();
  }
  function reset() {
    following.value = true;
    void nextTick(() => {
      if (following.value) latest();
    });
  }

  watch(
    content,
    () => {
      if (following.value) latest();
      else geometry();
    },
    { flush: "post" },
  );
  watch(
    viewport,
    (element) => {
      observer?.disconnect();
      if (!element) return;
      observer = new ResizeObserver(() => {
        if (following.value) latest();
        else geometry();
        positionChanged();
      });
      observer.observe(element);
      if (following.value) latest();
      positionChanged();
    },
    { flush: "post" },
  );
  onBeforeUnmount(() => observer?.disconnect());

  return { following, scroll, toggle, reset, resetPosition };
}
