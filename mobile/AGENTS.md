This is an Expo/React Native app (Android only). Prioritize mobile-first patterns and performance.

## Expo has changed — do not trust your training data

Expo ships breaking changes every SDK release; APIs you remember may be renamed, moved or removed. Before writing code that touches an Expo or React Native API:

1. Read the major version of `expo` in `package.json`.
2. Fetch the matching docs: `https://docs.expo.dev/versions/v<major>.0.0/`
3. For anything else, use https://docs.expo.dev/llms.txt (index of all Expo docs, with corrections to common LLM misconceptions) and follow its links; never answer from memory.

## Commands

```bash
npx expo install <package>  # ALWAYS use instead of npm add: resolves SDK-compatible versions
npx expo start              # dev server
npm run lint                # expo lint
npm run typecheck           # tsc --noEmit
npm test                    # jest (jest-expo)
npx expo-doctor             # diagnose dependency and config issues
```

Run lint, typecheck and the affected tests before declaring a task done.

## Navigation

Expo Router: routes live in `src/app/` (every file is a screen, `_layout.tsx` defines navigators). Keep non-route code outside `src/app/`. Import `Link`, `router` and `useLocalSearchParams` from `expo-router`.

## Rules

- `android/` is generated (gitignored): never edit it by hand. Change native behavior in `app.json`, config plugins or `modules/`. After adding a library with native code, rebuild with `npx expo run:android`; Expo Go only has its bundled modules.
- Prefer Expo modules over third-party libraries. Docs: https://docs.expo.dev/versions/latest/index.md
