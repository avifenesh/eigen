// A single shared 1Hz clock. Views that show live elapsed times read `now.ms`
// instead of each spinning their own interval — one timer for the whole app,
// started lazily on first read and never torn down (module singleton for the
// app lifetime, like the router's hashchange listener). Cheap: one setInterval.
function createClock() {
  let ms = $state(Date.now());
  let started = false;

  function ensure() {
    if (started) return;
    started = true;
    setInterval(() => {
      ms = Date.now();
    }, 1000);
  }

  return {
    get ms() {
      ensure();
      return ms;
    },
  };
}

export const now = createClock();
