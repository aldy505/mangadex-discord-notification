<script setup lang="ts">
import { RouterView } from 'vue-router'
import AppShell from './components/AppShell.vue'
import ToastHost from './components/ToastHost.vue'
import { session } from './lib/auth'
</script>

<template>
  <AppShell v-if="session.stage === 'full'">
    <RouterView />
  </AppShell>
  <RouterView v-else />
  <ToastHost />
</template>
