import { useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useRevealGroup } from '@/hooks/motion';
import { useThemeStore } from '@/stores';
import iconVertex from '@/assets/icons/vertex.svg';
import { OAuthHeader } from './components/OAuthHeader';
import { ProviderTile } from './components/ProviderTile';
import { OAuthFlowDialog, type DialogHeading } from './components/OAuthFlowDialog';
import { VertexImportDialog } from './components/VertexImportDialog';
import { useOAuthFlows, type ProviderFlowState } from './hooks/useOAuthFlows';
import { useVertexImport } from './hooks/useVertexImport';
import {
  isSponsor,
  OAUTH_PROVIDERS,
  resolveThemedIcon,
  supportsManualCallback,
  type OAuthProviderCard,
} from './providers';
import styles from './OAuthPage.module.scss';

/** Reserve a dialog identifier for Vertex, which is not an OAuth provider. */
const VERTEX_DIALOG_ID = '__vertex__';
const VERTEX_LABEL = 'Vertex AI';

type DialogTarget = { kind: 'oauth'; id: string } | { kind: 'vertex' };

/** Provider gallery with focused OAuth dialogs. Authorization continues after closing a dialog; retain the last target through the exit animation. */
export function OAuthPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const revealRef = useRevealGroup<HTMLDivElement>();
  const otherMethodsId = useId();

  const providerCards = OAUTH_PROVIDERS;

  const focusedRef = useRef<string | null>(null);
  const [dialog, setDialog] = useState<DialogTarget | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);

  const flows = useOAuthFlows({
    isFocused: (provider) => focusedRef.current === provider,
  });
  const vertexImport = useVertexImport({
    isFocused: () => focusedRef.current === VERTEX_DIALOG_ID,
    onCredentialAdded: flows.recordCredentialAdded,
  });

  const labelOf = (card: OAuthProviderCard) =>
    typeof card.label === 'string' ? card.label : t(card.label.key);

  const captionOf = (card: OAuthProviderCard) => {
    const method = t(
      card.flow === 'device' ? 'auth_login.method_device' : 'auth_login.method_browser'
    );
    return card.domain ? `${card.domain} · ${method}` : method;
  };

  const iconOf = (card: OAuthProviderCard) => resolveThemedIcon(card.icon, resolvedTheme);

  const headingOf = (card: OAuthProviderCard): DialogHeading => ({
    title: t(card.titleKey),
    caption: captionOf(card),
    icon: iconOf(card),
  });

  const sponsorOf = (card: OAuthProviderCard) =>
    isSponsor(card)
      ? { url: card.sponsor.signUpUrl, label: t('auth_login.kimi_sign_up_button') }
      : undefined;

  const tileStatusLabel = (status: ProviderFlowState['status']) =>
    status === 'waiting'
      ? t('auth_login.tile_waiting')
      : status === 'success'
        ? t('auth_login.tile_success')
        : status === 'error'
          ? t('auth_login.tile_error')
          : undefined;

  const openProvider = (card: OAuthProviderCard) => {
    focusedRef.current = card.id;
    setDialog({ kind: 'oauth', id: card.id });
    setDialogOpen(true);
    // Reopen an existing attempt; otherwise begin authorization immediately.
    if (!flows.states[card.id]?.status) void flows.startAuth(card.id);
  };

  const openVertex = () => {
    focusedRef.current = VERTEX_DIALOG_ID;
    setDialog({ kind: 'vertex' });
    setDialogOpen(true);
  };

  const closeDialog = () => {
    if (dialog?.kind === 'oauth') flows.dismissSuccess(dialog.id);
    if (dialog?.kind === 'vertex' && vertexImport.vertex.result) vertexImport.resetForAnother();
    focusedRef.current = null;
    setDialogOpen(false);
  };

  const viewAuthFiles = () => navigate('/auth-files');

  const dialogCard =
    dialog?.kind === 'oauth' ? providerCards.find((card) => card.id === dialog.id) : undefined;
  const providerCount = providerCards.length;
  const sponsorCards = providerCards.filter(isSponsor);
  const regularCards = providerCards.filter((card) => !isSponsor(card));

  const renderTile = (card: OAuthProviderCard) => {
    const status = flows.states[card.id]?.status;
    return (
      <ProviderTile
        key={card.id}
        label={labelOf(card)}
        caption={captionOf(card)}
        icon={iconOf(card)}
        index={providerCards.indexOf(card)}
        status={status}
        statusLabel={tileStatusLabel(status)}
        sponsor={sponsorOf(card)}
        onOpen={() => openProvider(card)}
      />
    );
  };
  const waitingCount = Object.values(flows.states).filter(
    (state) => state.status === 'waiting'
  ).length;

  return (
    <div className={styles.page} ref={revealRef}>
      <OAuthHeader
        providerCount={providerCount}
        waitingCount={waitingCount}
        addedCount={flows.addedCount}
      />

      {/* Sponsored tiles occupy the first row; other providers use the compact grid. */}
      {sponsorCards.length > 0 && (
        <div className={styles.sponsorGrid}>{sponsorCards.map(renderTile)}</div>
      )}
      <div className={styles.grid}>{regularCards.map(renderTile)}</div>

      <section className={styles.section} aria-labelledby={otherMethodsId}>
        <h2 className={styles.sectionLabel} id={otherMethodsId} data-reveal>
          {t('auth_login.other_login_methods')}
        </h2>
        <div className={styles.grid}>
          <ProviderTile
            label={VERTEX_LABEL}
            caption={t('vertex_import.file_label')}
            icon={iconVertex}
            index={providerCount}
            onOpen={openVertex}
          />
        </div>
      </section>

      {dialogCard && (
        <OAuthFlowDialog
          open={dialogOpen && dialog?.kind === 'oauth'}
          providerId={dialogCard.id}
          heading={headingOf(dialogCard)}
          state={flows.states[dialogCard.id] ?? {}}
          text={(suffix) => flows.providerText(dialogCard.id, suffix)}
          supportsCallback={supportsManualCallback(dialogCard)}
          sponsor={sponsorOf(dialogCard)}
          onStart={() => void flows.startAuth(dialogCard.id)}
          onCancel={() => void flows.cancelAuth(dialogCard.id)}
          onCallbackChange={(value) => flows.setCallbackInput(dialogCard.id, value)}
          onCallbackSubmit={() => void flows.submitCallback(dialogCard.id)}
          onViewAuthFiles={viewAuthFiles}
          onClose={closeDialog}
        />
      )}

      <VertexImportDialog
        open={dialogOpen && dialog?.kind === 'vertex'}
        heading={{
          title: t('vertex_import.title'),
          caption: t('vertex_import.file_label'),
          icon: iconVertex,
        }}
        vertex={vertexImport.vertex}
        onPickFile={vertexImport.pickFile}
        onLocationChange={vertexImport.setLocation}
        onImport={() => void vertexImport.importCredential()}
        onImportAnother={vertexImport.resetForAnother}
        onViewAuthFiles={viewAuthFiles}
        onClose={closeDialog}
      />
    </div>
  );
}
