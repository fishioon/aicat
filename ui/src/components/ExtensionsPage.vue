<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import {
  state,
  shell,
  navigate,
  popup,
  run,
  say,
  modal,
  closeDialog,
  showCertificateRecovery,
  busy,
  ask,
  refresh,
  problem,
} from "../state/desktop";
import { copy } from "../state/traffic";
import { host } from "../host";
import { t } from "../i18n";
import { extensionList, uiOf } from "../extensions";
import type { ExtensionState, Source } from "../generated/mygo";
import Icon from "./Icon.vue";
import ExtensionIcon from "./ExtensionIcon.vue";

const packageForm = computed({
  get: () => shell.customInstallOpen,
  set: (value: boolean) => {
    shell.customInstallOpen = value;
  },
});
const sourceInfo = ref<Source | null>(null);
const sourceHint = computed(() =>
  sourceInfo.value
    ? t(
        (
          {
            npm: "NPM 包",
            github: "GitHub 仓库",
            archive: "安装包地址",
          } as Record<string, string>
        )[sourceInfo.value.kind] || "",
      )
    : t(
        "例如 @publisher/channel、github:owner/repo 或 https://example.com/extension.tgz",
      ),
);
const sourceParseError = ref("");
const sourceError = ref("");
const ignoredSourceOperation = ref("");
const submittingSource = ref(false);
let sourceGeneration = 0;
let active = true;
watch(
  () => shell.customInstallSource,
  async (source, previous) => {
    const previousKind = sourceInfo.value?.kind;
    if (previous !== undefined)
      ignoredSourceOperation.value =
        state.value?.extensionInstallation.startedAt ?? "";
    const generation = ++sourceGeneration;
    sourceInfo.value = null;
    sourceParseError.value = "";
    sourceError.value = "";
    if (!source.trim()) return;
    try {
      const parsed = await host.parseExtensionSource({ source, version: "" });
      if (generation === sourceGeneration) {
        if (previousKind && previousKind !== parsed.kind)
          shell.customInstallVersion = "";
        sourceInfo.value = parsed;
      }
    } catch (error) {
      if (generation === sourceGeneration)
        sourceParseError.value = String(
          error instanceof Error ? error.message : error,
        );
    }
  },
  { immediate: true },
);
watch(
  () => shell.customInstallVersion,
  () => {
    sourceError.value = "";
  },
);
const sourceOperation = computed(() => state.value?.extensionInstallation);
const installingSource = computed(
  () => submittingSource.value || sourceOperation.value?.status === "running",
);
const sourceFailure = computed(() =>
  installingSource.value
    ? ""
    : sourceError.value ||
      (sourceOperation.value?.status === "failed" &&
      sourceOperation.value.startedAt !== ignoredSourceOperation.value
        ? sourceOperation.value.error
        : ""),
);
const sourceFailureText = computed(() => {
  const match = sourceFailure.value.match(/^(.*)（HTTP (\d{3})）$/);
  return match
    ? t("{message}（HTTP {status}）", {
        message: t(match[1]),
        status: match[2],
      })
    : t(sourceFailure.value);
});
const sourceElapsed = computed(() => {
  const started = Date.parse(sourceOperation.value?.startedAt ?? "");
  const end =
    sourceOperation.value?.status === "running"
      ? now.value
      : Date.parse(sourceOperation.value?.finishedAt ?? "");
  return Number.isFinite(started) && Number.isFinite(end)
    ? Math.max(0, Math.floor((end - started) / 1000))
    : 0;
});
async function addSource() {
  if (installingSource.value || !sourceInfo.value) return;
  sourceError.value = "";
  submittingSource.value = true;
  try {
    const id = await host.installExtensionSource({
      source: shell.customInstallSource.trim(),
      version:
        sourceInfo.value.kind === "archive"
          ? ""
          : shell.customInstallVersion.trim(),
    });
    await refresh();
    if (active && shell.route === "extensions" && packageForm.value) choose(id);
    say(t("插件已安装，请配置并验证账号"));
  } catch (e) {
    await refresh();
    if (sourceOperation.value?.status !== "cancelled")
      sourceError.value = e instanceof Error ? e.message : String(e);
  } finally {
    submittingSource.value = false;
    await refresh();
  }
}
async function cancelSource() {
  try {
    await host.cancelExtensionSourceInstallation();
    await refresh();
  } catch (error) {
    problem(error);
  }
}
const sections = reactive({ enabled: true, recommended: true });
const current = computed(
  () =>
    extensionList().find((x) => x.id === shell.extension) ?? extensionList()[0],
);
const ui = computed(() => uiOf(current.value?.id ?? ""));
const enabledExtensions = computed(() =>
  matches.value.filter((x) => x.enabled),
);
const recommended = computed(() => matches.value.filter((x) => !x.enabled));
const submitting = ref("");
const enabling = ref("");
const enableErrors = reactive<Record<string, string>>({});
const now = ref(Date.now());
const timer = setInterval(() => {
  now.value = Date.now();
}, 1000);
onUnmounted(() => {
  active = false;
  clearInterval(timer);
});
const operation = computed(() => current.value?.operation);
const installing = computed(
  () =>
    operation.value?.status === "running" ||
    submitting.value === current.value?.id,
);
const elapsed = computed(() => {
  const started = Date.parse(operation.value?.startedAt ?? "");
  const end =
    operation.value?.status === "running"
      ? now.value
      : Date.parse(operation.value?.finishedAt ?? "");
  return Number.isFinite(started) && Number.isFinite(end)
    ? Math.max(0, Math.floor((end - started) / 1000))
    : 0;
});
const installationPhase = (stage?: string) =>
  t(
    (
      {
        preparing: "正在准备安装…",
        downloading: "正在下载插件与依赖…",
        verifying: "正在校验插件…",
        activating: "正在完成安装…",
        removing: "正在卸载扩展…",
        cancelling: "正在取消安装…",
      } as Record<string, string>
    )[stage ?? ""] ?? "正在准备安装…",
  );
const phase = computed(() => installationPhase(operation.value?.stage));
const result = computed(() => {
  const op = operation.value;
  if (op?.status === "failed")
    return t(
      op.action === "update"
        ? "更新失败，原版本仍可使用。"
        : op.action === "uninstall"
          ? "卸载未完成，请重试。"
          : "安装失败，请重试。",
    );
  if (op?.status === "cancelled")
    return t(
      op.action === "update" ? "更新已取消，原版本仍可使用。" : "安装已取消。",
    );
  if (op?.status === "completed")
    return t(
      op.action === "uninstall"
        ? "扩展已卸载，配置和数据已保留"
        : op.action === "update"
          ? "插件已更新，请重新验证账号后启用"
          : "插件已安装，请配置并验证账号",
    );
  return "";
});
async function packageAction(action: "install" | "update") {
  const x = current.value;
  if (!x || installing.value) return;
  submitting.value = x.id;
  try {
    await (action === "update"
      ? host.updateExtension(x.id)
      : host.installExtension(x.id));
    await refresh();
    say(
      t(
        action === "update"
          ? "插件已更新，请重新验证账号后启用"
          : "插件已安装，请配置并验证账号",
      ),
    );
  } catch (error) {
    await refresh().catch(() => {});
    const op = state.value?.extensions.find(
      (item) => item.id === x.id,
    )?.operation;
    if (op?.status !== "failed" && op?.status !== "cancelled") problem(error);
  } finally {
    submitting.value = "";
  }
}
const install = () => packageAction("install");
const update = () => packageAction("update");
async function cancelInstall() {
  if (!current.value) return;
  try {
    await host.cancelExtensionInstallation(current.value.id);
    await refresh();
  } catch (error) {
    problem(error);
  }
}
function uninstall() {
  const x = current.value;
  if (!x) return;
  ask("卸载扩展", "删除扩展安装包，保留配置、凭据和消息数据。", async () => {
    if (await run(() => host.uninstallExtension(x.id))) {
      navigate("extensions", { extension: x.id });
      say(t("扩展已卸载，配置和数据已保留"));
    }
  });
}
const diagnostics = computed(() => shell.route === "extension-diagnostics");
const enabled = computed(() => !!current.value?.enabled);
const status = computed(() => {
  const own = ui.value.status?.();
  if (own || enabled.value) return own;
  return { text: t("这个扩展已停用。") };
});
const matches = computed(() => {
  const terms = shell.extensionQuery
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  const tags = terms.filter((term) => term.startsWith("tag:"));
  const words = terms.filter((term) => !term.startsWith("tag:"));
  return extensionList().filter((x) => {
    if (
      !tags.every(
        (tag) => tag === "tag:channel" && x.categories.includes("消息渠道"),
      )
    )
      return false;
    const text = [
      t(x.name),
      x.id,
      "aicat",
      t(x.summary),
      ...x.categories.map((c) => t(c)),
    ]
      .join(" ")
      .toLowerCase();
    return words.every((word) => text.includes(word));
  });
});
function choose(id: string) {
  packageForm.value = false;
  navigate("extensions", { extension: id });
}
async function set(x: ExtensionState, on: boolean) {
  enableErrors[x.id] = "";
  if (on) enabling.value = x.id;
  try {
    if (
      !(await run(async () => {
        try {
          await host.setExtensionEnabled(x.id, on);
        } catch (error) {
          enableErrors[x.id] =
            error instanceof Error ? error.message : String(error);
          throw error;
        }
      }))
    )
      return;
  } finally {
    enabling.value = "";
  }
  const done = t(on ? "已启用 {name}" : "已停用 {name}", { name: t(x.name) });
  say(
    x.restart
      ? t("{action}，{restart}", { action: done, restart: t(x.restart) })
      : done,
  );
}
// An extension that decrypts HTTPS is accepted by its client only once the
// root certificate is trusted: install it first, then turn it on.
function toggle() {
  const x = current.value;
  if (!x) return;
  if (x.enabled) return set(x, false);
  if (!x.needsTrust) return set(x, true);
  if (showCertificateRecovery()) return;
  if (state.value?.certificate.trusted) return set(x, true);
  modal.value = {
    title: "需要先安装证书",
    message: t(
      "启用后，aicat 会解密这个扩展需要的 HTTPS 连接，需要先安装并信任本机根证书。安装后自动启用这个扩展。",
    ),
    actions: [
      { label: "取消", run: closeDialog },
      {
        label: "安装证书",
        run: async () => {
          closeDialog();
          if (await run(() => host.installCertificate())) await set(x, true);
        },
      },
    ],
  };
}
function gear(event: MouseEvent) {
  const id = current.value?.id ?? "";
  popup(event.currentTarget as Element, [
    ...(!diagnostics.value && current.value?.installed && ui.value.Diagnostics
      ? [
          {
            label: "诊断",
            id: "extension-menu-diagnostics",
            run: () => navigate("extension-diagnostics", { extension: id }),
          },
        ]
      : []),
    ...(current.value?.packagePresent
      ? [
          {
            label: "卸载扩展",
            id: "extension-menu-uninstall",
            disabled: enabled.value || busy.value || installing.value,
            run: uninstall,
          },
        ]
      : []),
    ...(ui.value.menu?.() ?? []),
    {
      label: "复制扩展标识符",
      run: () => copy("extension-id", id),
    },
  ]);
}
</script>
<template>
  <div class="split" v-if="state">
    <aside class="side" id="module-sidebar" :aria-label="t('扩展列表')">
      <label class="search"
        ><span class="sr">{{ t("在扩展中搜索") }}</span
        ><Icon name="search" /><input
          id="extension-search"
          v-model="shell.extensionQuery"
          class="field"
          :placeholder="t('在扩展中搜索')"
          autocomplete="off"
          spellcheck="false"
      /></label>
      <section>
        <button
          class="xsec"
          id="extensions-enabled"
          :aria-expanded="sections.enabled"
          @click="sections.enabled = !sections.enabled"
        >
          <Icon name="chev" /><span class="label">{{ t("已启用") }}</span
          ><span class="num">{{ enabledExtensions.length }}</span>
        </button>
        <template v-if="sections.enabled">
          <ul v-if="enabledExtensions.length">
            <li v-for="x in enabledExtensions" :key="x.id">
              <button
                :id="'extension-' + x.id"
                class="xitem"
                :aria-current="
                  !packageForm && current?.id === x.id ? 'true' : undefined
                "
                @click="choose(x.id)"
              >
                <ExtensionIcon :name="x.id" /><span class="top"
                  ><strong>{{ t(x.name) }}</strong></span
                ><span class="desc">{{ t(x.summary) }}</span>
              </button>
            </li>
          </ul>
          <p v-else class="xnote">{{ t("没有匹配的扩展。") }}</p>
        </template>
      </section>
      <section>
        <button
          class="xsec"
          id="extensions-recommended"
          :aria-expanded="sections.recommended"
          @click="sections.recommended = !sections.recommended"
        >
          <Icon name="chev" /><span class="label">{{ t("推荐") }}</span
          ><span class="num">{{ recommended.length }}</span>
        </button>
        <ul v-if="sections.recommended && recommended.length">
          <li v-for="x in recommended" :key="x.id">
            <button
              :id="'extension-' + x.id"
              class="xitem"
              :aria-current="
                !packageForm && current?.id === x.id ? 'true' : undefined
              "
              @click="choose(x.id)"
            >
              <ExtensionIcon :name="x.id" /><span class="top"
                ><strong>{{ t(x.name) }}</strong></span
              ><span class="desc">{{ t(x.summary) }}</span>
            </button>
          </li>
        </ul>
      </section>
      <footer class="extension-install-entry">
        <button
          class="side-item"
          id="channel-package-add"
          :aria-current="packageForm ? 'true' : undefined"
          @click="packageForm = true"
        >
          <Icon name="plus" /><span class="label">{{ t("自定义安装") }}</span>
        </button>
      </footer>
    </aside>
    <section class="main">
      <div v-if="packageForm" class="xd">
        <h1>{{ t("自定义安装") }}</h1>
        <p>{{ t("填写 NPM 包名、公开 GitHub 仓库或安装包地址。") }}</p>
        <section class="extension-panel">
          <form
            class="form custom-install-form"
            id="channel-package-form"
            @submit.prevent="addSource"
          >
            <label for="custom-source">{{ t("安装来源") }}</label>
            <div class="acts">
              <input
                id="custom-source"
                class="field grow"
                v-model="shell.customInstallSource"
                :placeholder="t('NPM 包名、GitHub 仓库或安装包地址')"
                :disabled="installingSource"
                autocomplete="off"
                spellcheck="false"
                :aria-invalid="!!sourceParseError"
                aria-describedby="custom-source-hint"
              />
              <button
                class="btn primary"
                id="custom-install"
                :disabled="!sourceInfo || installingSource"
              >
                {{ t(installingSource ? "安装中…" : "安装") }}
              </button>
            </div>
            <p
              class="wide custom-source-hint"
              id="custom-source-hint"
              aria-live="polite"
            >
              {{ sourceParseError ? t(sourceParseError) : sourceHint }}
            </p>
            <details
              v-if="sourceInfo?.kind !== 'archive'"
              class="wide"
              id="custom-more-options"
            >
              <summary>{{ t("更多选项") }}</summary>
              <div class="form">
                <label for="custom-version">{{ t("版本") }}</label>
                <input
                  id="custom-version"
                  class="field"
                  v-model="shell.customInstallVersion"
                  :disabled="installingSource"
                  :placeholder="
                    t(
                      sourceInfo?.kind === 'github'
                        ? '分支、标签或 commit；留空使用默认分支'
                        : '固定版本或标签；留空使用 latest',
                    )
                  "
                  autocomplete="off"
                  spellcheck="false"
                />
              </div>
            </details>
            <section
              v-if="
                installingSource ||
                sourceOperation?.status === 'cancelled' ||
                sourceFailure
              "
              class="wide extension-install-status"
              id="custom-install-status"
              :aria-busy="installingSource"
              aria-live="polite"
            >
              <template v-if="installingSource">
                <p>{{ installationPhase(sourceOperation?.stage) }}</p>
                <progress
                  :aria-label="installationPhase(sourceOperation?.stage)"
                ></progress>
                <p>{{ t("已用 {seconds} 秒", { seconds: sourceElapsed }) }}</p>
                <button
                  v-if="sourceOperation?.cancellable"
                  class="btn"
                  id="custom-install-cancel"
                  type="button"
                  @click="cancelSource"
                >
                  {{ t("取消安装") }}
                </button>
              </template>
              <p
                v-else-if="
                  sourceOperation?.status === 'cancelled' && !sourceFailure
                "
              >
                {{ t("安装已取消。") }}
              </p>
              <p v-if="sourceFailure" role="alert" id="custom-install-error">
                {{ sourceFailureText }}
              </p>
            </section>
            <div class="acts wide">
              <button class="btn" type="button" @click="packageForm = false">
                {{ t("返回扩展") }}
              </button>
            </div>
          </form>
        </section>
      </div>
      <div
        v-else-if="current"
        class="xd extension-detail"
        :data-extension="current.id"
      >
        <header class="xd-head">
          <ExtensionIcon class="extension-emblem" :name="current.id" />
          <h1>{{ t(current.name) }}</h1>
          <p class="lede">{{ t(current.lede) }}</p>
          <div class="xd-acts">
            <button
              v-if="!current.installed"
              id="extension-install"
              class="xbtn primary"
              :disabled="busy || installing"
              @click="install"
            >
              {{
                installing
                  ? t("安装中…")
                  : operation?.status === "failed" ||
                      operation?.status === "cancelled"
                    ? t("重新安装")
                    : t("安装")
              }}
            </button>
            <button
              v-else
              :id="'feature-' + current.id"
              class="xbtn"
              :class="{ primary: !enabled }"
              :disabled="
                busy ||
                installing ||
                (!enabled && !current.ready && !current.canPrepare)
              "
              @click="toggle"
            >
              {{
                t(
                  enabling === current.id
                    ? "正在启用…"
                    : enabled
                      ? "停用"
                      : "启用",
                )
              }}
            </button>
            <button
              v-if="current.updateAvailable"
              id="extension-update"
              class="xbtn"
              :disabled="enabled || busy || installing"
              @click="update"
            >
              {{
                installing
                  ? t("更新中…")
                  : current.packageSource !== "registry"
                    ? t("检查更新")
                    : t("更新至 {version}", { version: current.version })
              }}
            </button>
            <button
              v-if="diagnostics"
              id="extension-diagnostics-configure"
              class="xbtn"
              @click="navigate('extensions', { extension: current.id })"
            >
              {{ t("配置") }}
            </button>
            <button
              v-if="!diagnostics && ui.Diagnostics && current.installed"
              id="extension-diagnostics"
              class="xbtn"
              @click="
                navigate('extension-diagnostics', { extension: current.id })
              "
            >
              {{ t("诊断") }}
            </button>
            <button
              id="extension-menu"
              class="icon-btn"
              aria-haspopup="menu"
              :aria-label="t('更多操作')"
              :title="t('更多操作')"
              @click="gear"
            >
              <Icon name="gear" />
            </button>
          </div>
          <section
            v-if="installing || result"
            id="extension-install-status"
            class="extension-install-status"
            :aria-busy="installing"
          >
            <p role="status" aria-live="polite">
              {{ installing ? phase : result }}
            </p>
            <template v-if="installing">
              <progress :aria-label="phase"></progress>
              <p class="muted">
                {{ t("已用 {seconds} 秒", { seconds: elapsed }) }}
                <template v-if="operation?.stage === 'downloading'">
                  · {{ t("下载受网络影响，最长等待 3 分钟。") }}</template
                >
              </p>
              <button
                v-if="operation?.cancellable"
                id="extension-install-cancel"
                class="xbtn"
                @click="cancelInstall"
              >
                {{ t("取消安装") }}
              </button>
            </template>
            <details v-if="operation?.status === 'failed'">
              <summary>{{ t("错误详情") }}</summary>
              <pre>{{ operation.error }}</pre>
            </details>
          </section>
          <p
            v-if="enableErrors[current.id]"
            id="extension-enable-error"
            role="alert"
            class="xd-status"
          >
            {{ enableErrors[current.id] }}
          </p>
          <p v-if="status && !installing" class="xd-status" :id="status.id">
            <Icon name="info" />{{ status.text }}
          </p>
        </header>
        <section
          v-if="
            current.installed && (diagnostics ? ui.Diagnostics : ui.Settings)
          "
          class="extension-panel"
          :id="
            diagnostics
              ? 'extension-diagnostics-page'
              : 'extension-settings-page'
          "
          :data-extension="current.id"
        >
          <component
            :is="diagnostics ? ui.Diagnostics : ui.Settings"
            :key="current.id + shell.route"
          />
        </section>
        <section v-if="!diagnostics && ui.Body" class="extension-panel">
          <component :is="ui.Body" :key="current.id" />
        </section>
        <details
          v-if="!diagnostics"
          :key="current.id"
          id="extension-information"
          class="extension-information"
        >
          <summary>{{ t("扩展信息") }}</summary>
          <dl>
            <dt>{{ t("标识符") }}</dt>
            <dd>
              <code>{{ current.id }}</code>
            </dd>
            <dt>{{ t("发布者") }}</dt>
            <dd>{{ current.publisher || "aicat" }}</dd>
            <dt>{{ t("来源") }}</dt>
            <dd>
              {{
                current.builtin
                  ? t("内置")
                  : current.packageFormat === "aicat"
                    ? "NPM"
                    : "OpenClaw"
              }}
            </dd>
            <dt>{{ t("版本") }}</dt>
            <dd>
              {{
                current.installedVersion ||
                current.version ||
                t("随 aicat 发布")
              }}
            </dd>
          </dl>
          <p
            v-if="current.package"
            id="extension-package-source"
            class="xd-meta"
          >
            <span
              >{{ t("下载来源") }}：{{
                current.packageOrigin || current.registry
              }}</span
            >
            <span
              ><code>{{ current.package }}@{{ current.version }}</code></span
            >
          </p>
        </details>
      </div>
    </section>
  </div>
</template>
