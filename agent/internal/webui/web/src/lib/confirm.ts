// One gate for every destructive admin action, so no page deletes, revokes or removes without asking.
// `ask` is injectable so the gate can be tested without a browser.
export function confirmDestructive(
  message: string,
  ask: (message: string) => boolean = (m) => window.confirm(m),
): boolean {
  return ask(message);
}
