// The background task is defined here, ahead of the router, because WorkManager's headless run evaluates this entry
// but never mounts the router's screens, and the task has to exist when the system asks for it.
import './src/features/background/backgroundTask';
import 'expo-router/entry';
