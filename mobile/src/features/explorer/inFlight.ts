/** Runs one async action at a time: a call made while one is still running is ignored and resolves to undefined. */
export class InFlightGate {
  private running = false;

  async run<T>(action: () => Promise<T>): Promise<T | undefined> {
    if (this.running) return undefined;
    this.running = true;
    try {
      return await action();
    } finally {
      this.running = false;
    }
  }
}
