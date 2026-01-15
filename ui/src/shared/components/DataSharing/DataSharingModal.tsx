import {
  Button,
  Content,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
} from "@patternfly/react-core";

interface DataSharingModalProps {
  isOpen: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  isLoading?: boolean;
}

function DataSharingModal({
  isOpen,
  onConfirm,
  onCancel,
  isLoading = false,
}: DataSharingModalProps) {
  return (
    <Modal
      isOpen={isOpen}
      onClose={onCancel}
      aria-labelledby="data-sharing-modal-title"
      aria-describedby="data-sharing-modal-body"
      variant="small"
    >
      <ModalHeader title="Share aggregated data" labelId="data-sharing-modal-title" />
      <ModalBody id="data-sharing-modal-body">
        <Content component="p">
          Unlock &lt;SaaS-only features&gt;.
        </Content>
        <Content component="p">
          By sharing aggregated data with Red Hat, you gain access to exclusive
          cloud capabilities and enhanced insights.
        </Content>
        <Content component="p">
          <strong>Important:</strong> This operation is permanent and cannot be undone.
        </Content>
        <Content component="p">
          Do you want to enable data sharing?
        </Content>
      </ModalBody>
      <ModalFooter>
        <Button
          variant="primary"
          onClick={onConfirm}
          isLoading={isLoading}
          isDisabled={isLoading}
        >
          Share
        </Button>
        <Button variant="link" onClick={onCancel} isDisabled={isLoading}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
}

export default DataSharingModal;
