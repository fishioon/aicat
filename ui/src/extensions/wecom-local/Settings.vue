<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { host } from "../../host";
import { busy, run, say, state } from "../../state/desktop";
import { t } from "../../i18n";
const channel = computed(() => state.value?.messageChannels["wecom-local"]);
const enabled = computed(
  () => !!state.value?.extensions.find((x) => x.id === "wecom-local")?.enabled,
);
const local = reactive({ accountId: "", dataDir: "" });
const rooms = ref<
  {
    room: string;
    name: string;
    direct: boolean;
    mention: boolean;
    mentionAll: boolean;
    all: boolean;
    keywords: string;
  }[]
>([]);
const agentEnabled = ref(false);
const agentBinary = ref("");
watch(
  () => channel.value?.agent,
  (value) => {
    agentEnabled.value = value?.enabled ?? false;
    agentBinary.value = value?.binary ?? "";
  },
  { immediate: true },
);
async function saveAgent() {
  if (
    await run(() =>
      host.updateExtensionConfig("wecom-local", {
        agent: { enabled: agentEnabled.value, binary: agentBinary.value },
      }),
    )
  )
    say(t("Codex 配置已保存"));
}
const key = ref("");
watch(
  () => JSON.stringify(channel.value?.local ?? null),
  () => {
    const value = channel.value?.local;
    Object.assign(local, value ?? { accountId: "", dataDir: "" });
    rooms.value = Object.entries(value?.replyRooms ?? {}).map(
      ([room, name]) => {
        const trigger = value?.triggers?.[room];
        return {
          room,
          name,
          direct: trigger?.direct ?? true,
          mention: trigger?.mention ?? true,
          mentionAll: trigger?.mentionAll ?? false,
          all: trigger ? trigger.all : true,
          keywords: trigger?.keywords?.join("\n") ?? "",
        };
      },
    );
  },
  { immediate: true },
);
async function save() {
  if (
    await run(() => {
      if (
        new Set(rooms.value.map((x) => x.room.trim())).size !==
        rooms.value.length
      )
        throw Error(t("自动处理 Room ID 不能重复"));
      return host.configureMessageChannel("wecom-local", {
        accountId: local.accountId,
        secret: key.value,
        local: {
          ...local,
          replyRooms: Object.fromEntries(
            rooms.value.map((x) => [x.room.trim(), x.name.trim()]),
          ),
          triggers: Object.fromEntries(
            rooms.value.map((x) => [
              x.room.trim(),
              {
                direct: x.direct,
                mention: x.mention,
                mentionAll: x.mentionAll,
                all: x.all,
                keywords: x.keywords
                  .split("\n")
                  .map((word) => word.trim())
                  .filter(Boolean),
              },
            ]),
          ),
        },
      });
    })
  ) {
    key.value = "";
    say(t("本地数据库验证通过，可以启用"));
  }
}
</script>
<template>
  <div id="wecom-local-settings">
    <h2>{{ t("本地数据库") }}</h2>
    <p>
      {{
        t(
          "填写含 message.db 的目录和 Base64 原始密钥。密钥单独保存在本机，留空可保留当前账号和目录的已保存密钥。",
        )
      }}
    </p>
    <p>
      {{
        t(
          "Agent 通过 MCP 请求发送文本。发送会操作企业微信窗口和剪贴板，需要辅助功能权限及可唯一搜索的会话名称。",
        )
      }}
    </p>
    <form class="form" id="wecom-local-config" @submit.prevent="save">
      <label for="wecom-local-account">{{ t("本地账号 ID") }}</label>
      <input
        id="wecom-local-account"
        class="field"
        v-model="local.accountId"
        :disabled="enabled || busy"
        autocomplete="off"
        required
      />
      <label for="wecom-local-dir">{{ t("数据库目录") }}</label>
      <input
        id="wecom-local-dir"
        class="field"
        v-model="local.dataDir"
        :disabled="enabled || busy"
        autocomplete="off"
        required
      />
      <label for="wecom-local-key">{{ t("Base64 原始密钥") }}</label>
      <input
        id="wecom-local-key"
        class="field"
        v-model="key"
        type="password"
        :placeholder="channel?.configured ? t('留空保留已保存的密钥') : ''"
        :disabled="enabled || busy"
        autocomplete="new-password"
        :required="!channel?.configured"
      />
      <p class="wide">
        {{
          t(
            "只有下列会话中满足触发条件的新文本才会调用 Agent；其他消息仍进入 Room，历史导入和自己的消息不会触发。",
          )
        }}
      </p>
      <div v-for="(route, index) in rooms" :key="index" class="form wide">
        <label :for="'wecom-local-room-' + index">{{
          t("自动处理 Room ID")
        }}</label>
        <input
          :id="'wecom-local-room-' + index"
          class="field"
          v-model="route.room"
          :disabled="enabled || busy"
          required
        />
        <label :for="'wecom-local-search-' + index">{{
          t("唯一会话搜索名")
        }}</label>
        <input
          :id="'wecom-local-search-' + index"
          class="field"
          v-model="route.name"
          :disabled="enabled || busy"
          required
        />
        <label class="wide"
          ><input
            :id="'wecom-local-all-' + index"
            type="checkbox"
            v-model="route.all"
            :disabled="enabled || busy"
          />{{ t("所有新文本（测试）") }}</label
        >
        <label class="wide"
          ><input
            :id="'wecom-local-direct-' + index"
            type="checkbox"
            v-model="route.direct"
            :disabled="enabled || busy || route.all"
          />{{ t("私聊新文本") }}</label
        >
        <label class="wide"
          ><input
            :id="'wecom-local-mention-' + index"
            type="checkbox"
            v-model="route.mention"
            :disabled="enabled || busy || route.all"
          />{{ t("群聊 @ 当前账号") }}</label
        >
        <label class="wide"
          ><input
            :id="'wecom-local-mention-all-' + index"
            type="checkbox"
            v-model="route.mentionAll"
            :disabled="enabled || busy || route.all"
          />{{ t("群聊 @所有人") }}</label
        >
        <label :for="'wecom-local-keywords-' + index">{{
          t("群聊关键词")
        }}</label>
        <textarea
          :id="'wecom-local-keywords-' + index"
          class="field"
          v-model="route.keywords"
          rows="3"
          :placeholder="t('每行一个关键词')"
          :disabled="enabled || busy || route.all"
        />
        <p class="wide">
          {{ t("任一条件满足即触发；关键词按文本包含匹配，忽略英文大小写。") }}
        </p>
        <div class="acts wide">
          <button
            class="btn"
            type="button"
            :disabled="enabled || busy"
            @click="rooms.splice(index, 1)"
          >
            {{ t("移除") }}
          </button>
        </div>
      </div>
      <div class="acts wide">
        <button
          id="wecom-local-add-room"
          class="btn"
          type="button"
          :disabled="enabled || busy || rooms.length >= 32"
          @click="
            rooms.push({
              room: '',
              name: '',
              direct: true,
              mention: true,
              mentionAll: false,
              all: false,
              keywords: '',
            })
          "
        >
          {{ t("添加自动处理会话") }}
        </button>
        <button id="wecom-local-save" class="btn" :disabled="enabled || busy">
          {{ t("保存并验证") }}
        </button>
      </div>
    </form>
    <form id="wecom-local-agent-form" class="form" @submit.prevent="saveAgent">
      <label class="wide"
        ><input
          id="wecom-local-agent-enabled"
          type="checkbox"
          v-model="agentEnabled"
          :disabled="enabled || busy"
        />{{ t("Codex 自动处理") }}</label
      >
      <label for="wecom-local-agent-binary">{{ t("Codex 可执行文件") }}</label>
      <input
        id="wecom-local-agent-binary"
        class="field"
        v-model="agentBinary"
        :disabled="enabled || busy"
        :placeholder="t('留空自动查找')"
      />
      <div class="acts wide">
        <button
          id="wecom-local-agent-save"
          class="btn"
          :disabled="enabled || busy"
        >
          {{ t("保存 Codex 配置") }}
        </button>
      </div>
    </form>
    <p v-if="enabled">
      {{ t("修改账号前请先停用扩展。停用保留账号和消息日志。") }}
    </p>
  </div>
</template>
