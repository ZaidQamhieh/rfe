/* Drives KWin's pointer through org_kde_kwin_fake_input (KWIN_WAYLAND_NO_PERMISSION_CHECKS=1).
 * Commands, one per argument: m:X,Y (absolute move)  d (button down)  u (button up)  s:MS (sleep)
 * l:X1,Y1,X2,Y2,N (N small moves along a line, 15 ms apart) */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <wayland-client.h>
#include "fake_input-client-protocol.h"
static struct org_kde_kwin_fake_input *fi;
static void reg(void *d, struct wl_registry *r, uint32_t name, const char *iface, uint32_t v) {
  if (!strcmp(iface, "org_kde_kwin_fake_input")) fi = wl_registry_bind(r, name, &org_kde_kwin_fake_input_interface, v < 5 ? v : 5);
}
static void rem(void *d, struct wl_registry *r, uint32_t n) {}
static const struct wl_registry_listener rl = { reg, rem };
int main(int argc, char **argv) {
  struct wl_display *dp = wl_display_connect(NULL); if (!dp) { fprintf(stderr, "no display\n"); return 1; }
  struct wl_registry *r = wl_display_get_registry(dp); wl_registry_add_listener(r, &rl, NULL); wl_display_roundtrip(dp);
  if (!fi) { fprintf(stderr, "no fake_input\n"); return 2; }
  org_kde_kwin_fake_input_authenticate(fi, "rfe-e2e", "drive the pointer in the e2e session");
  for (int i = 1; i < argc; i++) {
    char *a = argv[i]; double x, y, x2, y2; int n;
    if (sscanf(a, "m:%lf,%lf", &x, &y) == 2) org_kde_kwin_fake_input_pointer_motion_absolute(fi, wl_fixed_from_double(x), wl_fixed_from_double(y));
    else if (!strcmp(a, "d")) org_kde_kwin_fake_input_button(fi, 0x110, 1);
    else if (!strcmp(a, "u")) org_kde_kwin_fake_input_button(fi, 0x110, 0);
    else if (sscanf(a, "s:%d", &n) == 1) { wl_display_roundtrip(dp); usleep(n * 1000); }
    else if (sscanf(a, "l:%lf,%lf,%lf,%lf,%d", &x, &y, &x2, &y2, &n) == 5) for (int k = 1; k <= n; k++) { org_kde_kwin_fake_input_pointer_motion_absolute(fi, wl_fixed_from_double(x + (x2 - x) * k / n), wl_fixed_from_double(y + (y2 - y) * k / n)); wl_display_roundtrip(dp); usleep(15000); }
    wl_display_roundtrip(dp);
  }
  wl_display_roundtrip(dp); return 0;
}
