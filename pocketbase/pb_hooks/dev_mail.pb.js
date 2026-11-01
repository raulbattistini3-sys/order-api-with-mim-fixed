/// <reference path="../pb_data/types.d.ts" />

// With no SMTP server configured, print the reset link instead of failing to
// send it, so forgot-password works locally. Once SMTP is on, this does nothing.
onMailerRecordPasswordResetSend((e) => {
  if ($app.settings().smtp.enabled) {
    e.next();
    return;
  }
  console.log(`[dev mail] ${$app.settings().meta.appURL}/reset-password?token=${e.meta.token}`);
});
