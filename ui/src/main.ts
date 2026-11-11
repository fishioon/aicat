import { createApp } from "vue";
import App from "./App.vue";
import { initializeHost } from "./host";
import { metrics, remember } from "./state/traffic";
import { startDesktop, problem } from "./state/desktop";
import "./app.css";

async function main() {
  await initializeHost();
  const app = createApp(App);
  app.config.errorHandler = (error, _instance, info) => {
    remember(metrics.errors, {
      message: String(error),
      stack: error instanceof Error ? error.stack : "",
      info,
    });
    problem(error);
  };
  app.mount("#app");
  await startDesktop();
}
void main().catch(problem);
