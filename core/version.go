package core

// Version — Semver проекта.
// Правила:
//   MAJOR — кардинальные изменения протокола (ломает совместимость)
//   MINOR — новые фичи (Smart Bypass, Kill Switch, Web Panel и т.д.)
//   PATCH — багфиксы, мелкие улучшения, рефакторинг
// ⚠ При каждом коммите с изменением кода — проверь, нужно ли поднять версию!
//
// История:
//   0.0.1 — Phase 7b: Windows GUI (Wails+Svelte)
//   0.1.0 — Phase 7c: Web Management Panel
//   0.1.1 — Phase 8: Android MVP
//   0.2.0 — Phase A+B+C: Consolidation, Kill Switch, Reconnect, Android bypass
//   0.2.1 — Phase D: Cumulative counters, drop logging, error handling, web panel fix
//   0.2.2 — Keep-alive 5-15s, float64 loss, версионирование
//   0.2.3 — Ring-buffer loss tracking (скользящее окно вместо кумулятивных счётчиков)
const Version = "0.2.3"
