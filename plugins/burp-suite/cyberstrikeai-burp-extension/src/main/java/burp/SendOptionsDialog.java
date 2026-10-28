package burp;

import javax.swing.*;
import java.awt.*;
import java.util.List;

/**
 * Per-request send dialog: project, role, agent mode, and custom instruction.
 */
final class SendOptionsDialog {
    private SendOptionsDialog() {}

    static final class Result {
        final String instruction;
        final String projectId;
        final String role;
        final CyberStrikeAIClient.AgentMode agentMode;

        Result(String instruction, String projectId, String role, CyberStrikeAIClient.AgentMode agentMode) {
            this.instruction = instruction;
            this.projectId = projectId == null ? "" : projectId;
            this.role = role == null ? "" : role;
            this.agentMode = agentMode;
        }
    }

    private static String lastProjectId = "";
    private static String lastRole = "";
    private static CyberStrikeAIClient.AgentMode lastAgentMode = null;

    static Result show(
            Component parent,
            CyberStrikeAIClient client,
            CyberStrikeAIClient.Config cfg,
            String token,
            String defaultInstruction
    ) {
        CyberStrikeAIClient.AgentMode defaultMode = lastAgentMode != null
                ? lastAgentMode
                : CyberStrikeAIClient.AgentMode.EINO_SINGLE;

        JPanel panel = new JPanel(new BorderLayout(0, 10));
        panel.setBorder(BorderFactory.createEmptyBorder(4, 4, 0, 4));

        JPanel selectors = new JPanel(new GridBagLayout());
        GridBagConstraints gc = new GridBagConstraints();
        gc.insets = new Insets(4, 6, 4, 6);
        gc.fill = GridBagConstraints.HORIZONTAL;
        gc.weighty = 0;

        JComboBox<CyberStrikeAIClient.ProjectOption> projectBox = new JComboBox<>();
        projectBox.addItem(new CyberStrikeAIClient.ProjectOption("", "Loading..."));
        projectBox.setEnabled(false);

        JComboBox<CyberStrikeAIClient.RoleOption> roleBox = new JComboBox<>();
        roleBox.addItem(new CyberStrikeAIClient.RoleOption("", "Loading..."));
        roleBox.setEnabled(false);

        JComboBox<CyberStrikeAIClient.AgentMode> agentBox = new JComboBox<>(CyberStrikeAIClient.AgentMode.values());
        agentBox.setSelectedItem(defaultMode);
        agentBox.setRenderer(new DefaultListCellRenderer() {
            @Override
            public Component getListCellRendererComponent(JList<?> list, Object value, int index, boolean isSelected, boolean cellHasFocus) {
                super.getListCellRendererComponent(list, value, index, isSelected, cellHasFocus);
                if (value instanceof CyberStrikeAIClient.AgentMode) {
                    setText(((CyberStrikeAIClient.AgentMode) value).displayName);
                }
                return this;
            }
        });

        // Row 0: labels — Project | Role | Conversation
        gc.gridy = 0;
        gc.weightx = 1.0;

        gc.gridx = 0;
        selectors.add(new JLabel("Project"), gc);
        gc.gridx = 1;
        selectors.add(new JLabel("Role"), gc);
        gc.gridx = 2;
        selectors.add(new JLabel("Conversation"), gc);

        // Row 1: dropdowns
        gc.gridy = 1;

        gc.gridx = 0;
        selectors.add(projectBox, gc);
        gc.gridx = 1;
        selectors.add(roleBox, gc);
        gc.gridx = 2;
        selectors.add(agentBox, gc);

        JTextArea editor = new JTextArea(
                defaultInstruction == null || defaultInstruction.trim().isEmpty()
                        ? HttpMessageFormatter.defaultInstruction()
                        : defaultInstruction,
                7,
                70
        );
        editor.setLineWrap(true);
        editor.setWrapStyleWord(true);
        editor.setFont(new Font(Font.SANS_SERIF, Font.PLAIN, 13));

        JPanel instructionPanel = new JPanel(new BorderLayout(0, 6));
        instructionPanel.add(new JLabel("Test instruction (can be modified for this traffic):"), BorderLayout.NORTH);
        instructionPanel.add(new JScrollPane(editor), BorderLayout.CENTER);

        panel.add(selectors, BorderLayout.NORTH);
        panel.add(instructionPanel, BorderLayout.CENTER);

        Thread loader = new Thread(() -> {
            try {
                List<CyberStrikeAIClient.ProjectOption> projects = client.fetchProjects(cfg, token);
                List<CyberStrikeAIClient.RoleOption> roles = client.fetchRoles(cfg, token);
                SwingUtilities.invokeLater(() -> populateProjects(projectBox, projects, lastProjectId));
                SwingUtilities.invokeLater(() -> populateRoles(roleBox, roles, lastRole));
            } catch (Exception ex) {
                SwingUtilities.invokeLater(() -> {
                    projectBox.removeAllItems();
                    projectBox.addItem(new CyberStrikeAIClient.ProjectOption("", "(load failed)"));
                    projectBox.setEnabled(true);
                    roleBox.removeAllItems();
                    roleBox.addItem(new CyberStrikeAIClient.RoleOption("", "default"));
                    roleBox.setEnabled(true);
                    JOptionPane.showMessageDialog(parent,
                            "Failed to load projects/roles: " + ex.getMessage(),
                            "CyberStrikeAI", JOptionPane.WARNING_MESSAGE);
                });
            }
        }, "CyberStrikeAI-LoadCatalog");
        loader.start();

        int result = JOptionPane.showConfirmDialog(
                parent,
                panel,
                "Send to CyberStrikeAI",
                JOptionPane.OK_CANCEL_OPTION,
                JOptionPane.PLAIN_MESSAGE
        );
        if (result != JOptionPane.OK_OPTION) {
            return null;
        }

        CyberStrikeAIClient.ProjectOption project = (CyberStrikeAIClient.ProjectOption) projectBox.getSelectedItem();
        CyberStrikeAIClient.RoleOption role = (CyberStrikeAIClient.RoleOption) roleBox.getSelectedItem();
        CyberStrikeAIClient.AgentMode agentMode = (CyberStrikeAIClient.AgentMode) agentBox.getSelectedItem();
        if (agentMode == null) {
            agentMode = defaultMode;
        }

        String instruction = editor.getText();
        if (instruction == null || instruction.trim().isEmpty()) {
            instruction = HttpMessageFormatter.defaultInstruction();
        } else {
            instruction = instruction.trim();
        }

        lastProjectId = project != null ? project.id : "";
        lastRole = role != null ? role.name : "";
        lastAgentMode = agentMode;

        return new Result(instruction, lastProjectId, lastRole, agentMode);
    }

    private static void populateProjects(JComboBox<CyberStrikeAIClient.ProjectOption> box,
                                         List<CyberStrikeAIClient.ProjectOption> items,
                                         String selectId) {
        box.removeAllItems();
        for (CyberStrikeAIClient.ProjectOption item : items) {
            box.addItem(item);
        }
        selectComboById(box, selectId);
        box.setEnabled(true);
    }

    private static void populateRoles(JComboBox<CyberStrikeAIClient.RoleOption> box,
                                      List<CyberStrikeAIClient.RoleOption> items,
                                      String selectName) {
        box.removeAllItems();
        for (CyberStrikeAIClient.RoleOption item : items) {
            box.addItem(item);
        }
        selectComboByName(box, selectName);
        box.setEnabled(true);
    }

    private static void selectComboById(JComboBox<CyberStrikeAIClient.ProjectOption> box, String id) {
        if (id == null) id = "";
        for (int i = 0; i < box.getItemCount(); i++) {
            CyberStrikeAIClient.ProjectOption item = box.getItemAt(i);
            if (item != null && id.equals(item.id)) {
                box.setSelectedIndex(i);
                return;
            }
        }
        box.setSelectedIndex(0);
    }

    private static void selectComboByName(JComboBox<CyberStrikeAIClient.RoleOption> box, String name) {
        if (name == null) name = "";
        for (int i = 0; i < box.getItemCount(); i++) {
            CyberStrikeAIClient.RoleOption item = box.getItemAt(i);
            if (item != null && name.equals(item.name)) {
                box.setSelectedIndex(i);
                return;
            }
        }
        box.setSelectedIndex(0);
    }
}
