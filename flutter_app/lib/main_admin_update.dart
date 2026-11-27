part of 'main.dart';

// Updating the server from the admin page. The banner, the settings and
// following the restart are neolabs_flutter's; this supplies the server calls
// and the wording. The server runs the same flow as `neocloud update`.

NeoUpdateController _updateController(NeoCloudController controller) =>
    NeoUpdateController(
      NeoUpdateApi(
        status: controller.client.adminUpdateStatus,
        check: controller.client.adminCheckForUpdate,
        start: controller.client.adminStartUpdate,
        setChannel: controller.client.adminSetUpdateChannel,
        failureOf: (error) => error is ApiException
            ? (
                status: error.statusCode,
                message: controller.friendlyError(error),
              )
            : null,
      ),
    );

NeoUpdateStrings _updateStrings() => NeoUpdateStrings(
  title: appStrings.updatesTitle,
  description: appStrings.updatesHint,
  versionLabel: appStrings.updateVersionLabel,
  channelLabel: appStrings.updateChannel,
  channelHint: appStrings.updateChannelHint,
  stable: appStrings.updateChannelStable,
  beta: appStrings.updateChannelBeta,
  checkNow: appStrings.updateCheckNow,
  neverChecked: appStrings.updateNeverChecked,
  checkedAt: (time) => appStrings.updateLastChecked(
    formatDate(time, currentAppLanguage.code),
  ),
  upToDate: appStrings.updateUpToDate,
  checkFailed: appStrings.updateCheckFailed,
  details: appStrings.updateDetails,
  // A container image is the only install that cannot update itself.
  blocked: (_) => (
    title: appStrings.updateContainerTitle,
    hint: appStrings.updateContainerHint,
  ),
  readyTitle: appStrings.updateReadyTitle,
  readySubtitle: appStrings.updateReadySubtitle,
  whatsNew: appStrings.updateWhatsNew,
  updateNow: appStrings.updateNow,
  confirmTitle: appStrings.updateConfirmTitle,
  confirmMessage: appStrings.updateConfirmMessage,
  cancel: appStrings.cancel,
  updatingTitle: appStrings.updatingTitle,
  updatingToTitle: appStrings.updatingToTitle,
  reconnecting: appStrings.updateReconnecting,
  handsOff: appStrings.updateHandsOff,
  doneTitle: appStrings.updateDoneTitle,
  doneReloading: appStrings.updateDoneReloading,
  reload: appStrings.updateReload,
  failedTitle: appStrings.updateFailedTitle,
  close: appStrings.close,
  retry: appStrings.retry,
);
