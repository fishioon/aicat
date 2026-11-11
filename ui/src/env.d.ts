import type { DesktopState } from "./generated/mygo";
import type { navigate, refresh, problem } from "./state/desktop";
import type { metrics } from "./state/traffic";
declare global {
  interface Window {
    desktop: {
      navigate: typeof navigate;
      refresh: typeof refresh;
      problem: typeof problem;
      state: () => DesktopState | null;
    };
    trafficView: {
      metrics: typeof metrics;
      state: () => {
        total: number;
        visible: number;
        selected: number;
        ids: number[];
        domRows: number;
        focused: string | undefined;
        scrollTop: number;
        detailRevision: number;
      };
      status: () => { activeSubscriptions: number; transport: string };
    };
    __aicatPreview?: boolean;
  }
}
