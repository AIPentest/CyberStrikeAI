package burp;

import javax.swing.*;
import java.awt.*;
import java.awt.datatransfer.StringSelection;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

final class CyberStrikeAITab implements ITab {
    private final JPanel root = new JPanel(new BorderLayout());

    private final JTextField hostField = new JTextField("127.0.0.1");
    private final JTextField portField = new JTextField("8080");
    private final JCheckBox useHttpsBox = new JCheckBox("HTTPS", true);
    private final JTextField usernameField = new JTextField("admin");
    private final JPasswordField passwordField = new JPasswordField();
    private final JButton validateButton = new JButton("Validate");
    private final JButton clearButton = new JButton("Clear Output");
    private final JButton stopButton = new JButton("Stop");
    private final JButton copyButton = new JButton("Copy");
    private final JButton clearAllButton = new JButton("Clear All");
    private final JLabel statusLabel = new JLabel("Not validated");
    private final JCheckBox showDebugEventsBox = new JCheckBox("Show debug events", false);
    private final JCheckBox renderMarkdownBox = new JCheckBox("Render Markdown", true);

    private final JTextArea progressArea = new JTextArea();
    private final JTextArea finalRawArea = new JTextArea(); // raw final stream / final response
    private JScrollPane progressScrollPane;
    private JScrollPane finalRawScrollPane;
    /** 距底部在此像素内视为「跟随滚动」，否则用户上拉阅读时不抢滚动条 */
    private static final int SCROLL_FOLLOW_THRESHOLD_PX = 48;
    private final JEditorPane markdownPane = new JEditorPane("text/html", "");
    private final CardLayout outputCardsLayout = new CardLayout();
    private final JPanel outputCards = new JPanel(outputCardsLayout);
    private final JPanel outputRoot = new JPanel(new BorderLayout());
    private final JPanel progressContainer = new JPanel(new CardLayout());
    private final JToggleButton progressToggle = new JToggleButton("Progress ▾", true);
    private final JTextArea requestArea = new JTextArea();
    private final JTextArea responseArea = new JTextArea();
    private final JTabbedPane rightTabs = new JTabbedPane();

    private final CyberStrikeAIClient client = new CyberStrikeAIClient();
    private final AtomicReference<String> tokenRef = new AtomicReference<>("");
    private final AtomicReference<Thread> validateThreadRef = new AtomicReference<>();

    private final DefaultListModel<TestRun> testListModel = new DefaultListModel<>();
    private final JList<TestRun> testList = new JList<>(testListModel);
    private final DefaultListModel<TestRun> filteredListModel = new DefaultListModel<>();
    private final JList<TestRun> filteredList = new JList<>(filteredListModel);
    private final JTextField searchField = new JTextField();
    private final Map<String, TestRun> runs = new HashMap<>();
    private final Map<String, Integer> runIdToIndex = new HashMap<>();
    private final AtomicInteger runSeq = new AtomicInteger(1);
    private String selectedRunId = null;

    private static final class TestRun {
        final String id;
        final String title;
        final String agentMode;
        final StringBuilder buffer = new StringBuilder();
        final StringBuilder progressBuffer = new StringBuilder();
        final StringBuilder finalBuffer = new StringBuilder();
        final StringBuilder thinkingPending = new StringBuilder();
        String status;
        String conversationId;
        String requestRaw;
        String responseRaw;
        String finalResponse;

        TestRun(String id, String title, String agentMode) {
            this.id = id;
            this.title = title;
            this.agentMode = agentMode;
            this.status = "running";
            this.conversationId = "";
            this.requestRaw = "";
            this.responseRaw = "";
            this.finalResponse = "";
        }

        @Override
        public String toString() {
            return id;
        }
    }

    CyberStrikeAITab() {
        root.add(buildConfigPanel(), BorderLayout.NORTH);
        root.add(buildMainPane(), BorderLayout.CENTER);
        wireActions();
    }

    private JComponent buildConfigPanel() {
        // Best-practice toolbar layout:
        // Row 1 = connection settings
        // Row 2 = run controls + view options
        JPanel rootPanel = new JPanel();
        rootPanel.setLayout(new BoxLayout(rootPanel, BoxLayout.Y_AXIS));
        rootPanel.setBorder(BorderFactory.createEmptyBorder(4, 6, 4, 6));

        hostField.setColumns(14);
        portField.setColumns(6);
        usernameField.setColumns(10);
        passwordField.setColumns(12);

        JPanel row1 = new JPanel(new FlowLayout(FlowLayout.LEFT, 8, 2));
        row1.add(new JLabel("Host"));
        row1.add(hostField);
        row1.add(new JLabel("Port"));
        row1.add(portField);
        useHttpsBox.setToolTipText("Use https:// for CyberStrikeAI (self-signed certs are trusted automatically)");
        row1.add(useHttpsBox);
        row1.add(new JLabel("Username"));
        row1.add(usernameField);
        row1.add(new JLabel("Password"));
        row1.add(passwordField);
        row1.add(validateButton);
        row1.add(statusLabel);

        JPanel row2 = new JPanel(new FlowLayout(FlowLayout.LEFT, 8, 2));
        row2.add(stopButton);
        row2.add(copyButton);
        row2.add(clearButton);
        row2.add(showDebugEventsBox);
        row2.add(renderMarkdownBox);

        rootPanel.add(row1);
        rootPanel.add(row2);
        return rootPanel;
    }

    private JComponent buildMainPane() {
        JPanel sidebarPanel = buildSidebarPanel();
        JComponent right = buildRightPanel();

        JSplitPane split = new JSplitPane(JSplitPane.HORIZONTAL_SPLIT, sidebarPanel, right);
        split.setResizeWeight(0.25);
        split.setBorder(null);
        return split;
    }

    private JPanel buildSidebarPanel() {
        JPanel p = new JPanel(new BorderLayout());
        filteredList.setSelectionMode(ListSelectionModel.SINGLE_SELECTION);

        filteredList.setFont(new Font(Font.SANS_SERIF, Font.PLAIN, 12));
        filteredList.setCellRenderer(new TestRunCellRenderer());
        filteredList.addListSelectionListener(e -> {
            if (!e.getValueIsAdjusting()) {
                String id = getSelectedRunIdFromList();
                if (id != null) {
                    setLogAreaToRun(id);
                }
            }
        });

        JLabel title = new JLabel("Test History");
        title.setBorder(BorderFactory.createEmptyBorder(6, 8, 6, 8));

        JPanel top = new JPanel(new BorderLayout(8, 6));
        top.setBorder(BorderFactory.createEmptyBorder(0, 8, 0, 8));
        top.add(title, BorderLayout.NORTH);
        searchField.setToolTipText("Search runs (title)");
        top.add(searchField, BorderLayout.SOUTH);

        JScrollPane sp = new JScrollPane(filteredList);
        sp.setBorder(BorderFactory.createTitledBorder("Runs"));

        clearAllButton.addActionListener(e -> clearAllRuns());
        JPanel bottom = new JPanel(new FlowLayout(FlowLayout.LEFT, 8, 6));
        bottom.add(clearAllButton);

        p.add(top, BorderLayout.NORTH);
        p.add(sp, BorderLayout.CENTER);
        p.add(bottom, BorderLayout.SOUTH);
        p.setPreferredSize(new Dimension(320, 200));
        return p;
    }

    private JComponent buildRightPanel() {
        configureTextArea(progressArea, true);
        configureTextArea(finalRawArea, true);
        markdownPane.setEditable(false);
        markdownPane.putClientProperty(JEditorPane.HONOR_DISPLAY_PROPERTIES, Boolean.TRUE);
        markdownPane.setFont(new Font(Font.SANS_SERIF, Font.PLAIN, 12));
        markdownPane.setOpaque(true);
        markdownPane.setBackground(Color.WHITE);

        configureTextArea(requestArea, false);
        configureTextArea(responseArea, false);

        finalRawScrollPane = new JScrollPane(finalRawArea);
        finalRawScrollPane.setHorizontalScrollBarPolicy(ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER);
        finalRawScrollPane.getVerticalScrollBar().setUnitIncrement(16);
        outputCards.add(finalRawScrollPane, "raw");
        outputCards.add(new JScrollPane(markdownPane), "md");

        outputRoot.add(buildOutputHeader(), BorderLayout.NORTH);
        outputRoot.add(buildOutputBody(), BorderLayout.CENTER);

        rightTabs.addTab("Output", outputRoot);
        JScrollPane requestScroll = new JScrollPane(requestArea);
        requestScroll.setHorizontalScrollBarPolicy(ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER);
        rightTabs.addTab("Request", requestScroll);
        JScrollPane responseScroll = new JScrollPane(responseArea);
        responseScroll.setHorizontalScrollBarPolicy(ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER);
        rightTabs.addTab("Response", responseScroll);
        return rightTabs;
    }

    private JComponent buildOutputHeader() {
        JPanel header = new JPanel(new BorderLayout(8, 0));
        header.setBorder(BorderFactory.createEmptyBorder(6, 8, 6, 8));

        JPanel left = new JPanel(new FlowLayout(FlowLayout.LEFT, 8, 0));
        left.add(progressToggle);
        header.add(left, BorderLayout.WEST);

        return header;
    }

    private JComponent buildOutputBody() {
        progressScrollPane = new JScrollPane(progressArea);
        progressScrollPane.setBorder(BorderFactory.createTitledBorder("Progress"));
        progressScrollPane.setHorizontalScrollBarPolicy(ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER);
        progressScrollPane.getVerticalScrollBar().setUnitIncrement(16);

        JPanel empty = new JPanel();
        progressContainer.add(progressScrollPane, "show");
        progressContainer.add(empty, "hide");
        ((CardLayout) progressContainer.getLayout()).show(progressContainer, "show");

        JPanel finalPanel = new JPanel(new BorderLayout());
        finalPanel.add(outputCards, BorderLayout.CENTER);
        finalPanel.setBorder(BorderFactory.createTitledBorder("Final Response"));

        JSplitPane split = new JSplitPane(JSplitPane.VERTICAL_SPLIT, progressContainer, finalPanel);
        split.setResizeWeight(0.15);
        split.setBorder(null);
        split.setDividerSize(6);

        final int[] lastDividerLocation = new int[]{140}; // sensible default

        progressToggle.addActionListener(e -> {
            boolean show = progressToggle.isSelected();
            progressToggle.setText(show ? "Progress ▾" : "Progress ▸");
            CardLayout cl = (CardLayout) progressContainer.getLayout();
            cl.show(progressContainer, show ? "show" : "hide");
            if (!show) {
                int current = split.getDividerLocation();
                if (current > 0) {
                    lastDividerLocation[0] = current;
                }
                split.setDividerLocation(0);
                split.setDividerSize(0);
            } else {
                split.setDividerSize(6);
                // Restore previous divider location (or fallback to 20% of height)
                int restore = lastDividerLocation[0];
                if (restore <= 0) {
                    int h = split.getHeight();
                    restore = (h > 0) ? Math.max(80, (int) (h * 0.2)) : 140;
                }
                split.setDividerLocation(restore);
            }
            split.revalidate();
            split.repaint();
        });

        return split;
    }

    private static boolean isScrollNearBottom(JScrollPane scrollPane) {
        if (scrollPane == null) {
            return true;
        }
        JScrollBar bar = scrollPane.getVerticalScrollBar();
        int max = Math.max(0, bar.getMaximum() - bar.getVisibleAmount());
        return bar.getValue() >= max - SCROLL_FOLLOW_THRESHOLD_PX;
    }

    private static void scrollPaneToBottom(JScrollPane scrollPane) {
        if (scrollPane == null) {
            return;
        }
        JScrollBar bar = scrollPane.getVerticalScrollBar();
        bar.setValue(bar.getMaximum());
    }

    private static void configureTextArea(JTextArea area, boolean monospaced) {
        area.setEditable(false);
        area.setLineWrap(true);
        area.setWrapStyleWord(true);
        if (monospaced) {
            area.setFont(new Font(Font.MONOSPACED, Font.PLAIN, 12));
        } else {
            area.setFont(new Font(Font.MONOSPACED, Font.PLAIN, 12));
        }
    }

    private static Color colorForStatus(String status) {
        if (status == null) return new Color(120, 120, 120);
        switch (status) {
            case "running":
                return new Color(33, 150, 243);
            case "done":
                return new Color(76, 175, 80);
            case "error":
                return new Color(244, 67, 54);
            case "cancelled":
            case "cancelling":
                return new Color(255, 152, 0);
            default:
                return new Color(120, 120, 120);
        }
    }

    private static final class DotIcon implements Icon {
        private final int size;
        private Color color;

        DotIcon(int size, Color color) {
            this.size = size;
            this.color = color;
        }

        void setColor(Color color) {
            this.color = color;
        }

        @Override
        public int getIconWidth() {
            return size;
        }

        @Override
        public int getIconHeight() {
            return size;
        }

        @Override
        public void paintIcon(Component c, Graphics g, int x, int y) {
            Graphics2D g2 = (Graphics2D) g.create();
            try {
                g2.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
                g2.setColor(color != null ? color : Color.GRAY);
                g2.fillOval(x, y, size, size);
            } finally {
                g2.dispose();
            }
        }
    }

    private static final class TestRunCellRenderer implements ListCellRenderer<TestRun> {
        private final JPanel panel = new JPanel(new BorderLayout(8, 0));
        private final JLabel dotLabel = new JLabel();
        private final JLabel titleLabel = new JLabel();
        private final JLabel metaLabel = new JLabel();
        private final JPanel textPanel = new JPanel();
        private final DotIcon dotIcon = new DotIcon(10, new Color(120, 120, 120));

        TestRunCellRenderer() {
            panel.setBorder(BorderFactory.createEmptyBorder(6, 8, 6, 8));
            dotLabel.setIcon(dotIcon);

            textPanel.setLayout(new BoxLayout(textPanel, BoxLayout.Y_AXIS));
            titleLabel.setFont(titleLabel.getFont().deriveFont(Font.BOLD));
            metaLabel.setFont(metaLabel.getFont().deriveFont(Font.PLAIN, 11f));
            metaLabel.setForeground(new Color(102, 102, 102));
            textPanel.add(titleLabel);
            textPanel.add(metaLabel);

            panel.add(dotLabel, BorderLayout.WEST);
            panel.add(textPanel, BorderLayout.CENTER);
            panel.setOpaque(true);
            textPanel.setOpaque(false);
        }

        @Override
        public Component getListCellRendererComponent(JList<? extends TestRun> list, TestRun value, int index, boolean isSelected, boolean cellHasFocus) {
            String titleText = value != null ? value.title : "";
            String modeText = value != null ? value.agentMode : "";
            String statusText = value != null ? value.status : "";

            String shownTitle = titleText;
            if (shownTitle.length() > 80) {
                shownTitle = shownTitle.substring(0, 77) + "...";
            }
            titleLabel.setText(shownTitle);
            metaLabel.setText(modeText + " · " + statusText);

            dotIcon.setColor(colorForStatus(statusText));

            if (isSelected) {
                panel.setBackground(list.getSelectionBackground());
                titleLabel.setForeground(list.getSelectionForeground());
                metaLabel.setForeground(list.getSelectionForeground());
            } else {
                panel.setBackground(list.getBackground());
                titleLabel.setForeground(list.getForeground());
                metaLabel.setForeground(new Color(102, 102, 102));
            }

            return panel;
        }
    }

    // right panel builds scroll panes for each tab

    private void setConnectionEditable(boolean editable) {
        hostField.setEnabled(editable);
        portField.setEnabled(editable);
        useHttpsBox.setEnabled(editable);
        usernameField.setEnabled(editable);
        passwordField.setEnabled(editable);
    }

    private void invalidateAuthentication() {
        tokenRef.set("");
        statusLabel.setText("Connection changed — please Validate again");
    }

    private void wireActions() {
        javax.swing.event.DocumentListener credentialsChanged = new javax.swing.event.DocumentListener() {
            @Override public void insertUpdate(javax.swing.event.DocumentEvent e) { invalidateAuthentication(); }
            @Override public void removeUpdate(javax.swing.event.DocumentEvent e) { invalidateAuthentication(); }
            @Override public void changedUpdate(javax.swing.event.DocumentEvent e) { invalidateAuthentication(); }
        };
        for (JTextField field : new JTextField[]{hostField, portField, usernameField, passwordField}) {
            field.getDocument().addDocumentListener(credentialsChanged);
        }
        useHttpsBox.addActionListener(e -> invalidateAuthentication());
        validateButton.addActionListener(e -> {
            if ("Cancel".equals(validateButton.getText())) {
                cancelValidateInProgress();
                return;
            }
            final CyberStrikeAIClient.Config cfg = currentConfig();
            tokenRef.set("");
            setConnectionEditable(false);
            validateButton.setText("Cancel");
            validateButton.setEnabled(true);
            stopButton.setEnabled(true);
            statusLabel.setText("Validating...");
            log("Validating connection... (max ~10s; click Cancel or Stop to abort)");
            Thread worker = new Thread(() -> {
                try {
                    String token = client.loginAndValidate(cfg);
                    if (Thread.currentThread().isInterrupted()) {
                        return;
                    }
                    tokenRef.set(token);
                    SwingUtilities.invokeLater(() -> statusLabel.setText("OK (token saved)"));
                    log("Validation OK.");
                } catch (Exception ex) {
                    tokenRef.set("");
                    if (Thread.currentThread().isInterrupted()) {
                        SwingUtilities.invokeLater(() -> statusLabel.setText("Cancelled"));
                        log("Validation cancelled.");
                    } else {
                        SwingUtilities.invokeLater(() -> statusLabel.setText("Failed: " + ex.getMessage()));
                        log("Validation failed: " + ex.getMessage());
                    }
                } finally {
                    validateThreadRef.set(null);
                    SwingUtilities.invokeLater(() -> {
                        setConnectionEditable(true);
                        validateButton.setText("Validate");
                        validateButton.setEnabled(true);
                    });
                }
            }, "CyberStrikeAI-Validate");
            validateThreadRef.set(worker);
            worker.start();
        });

        clearButton.addActionListener(e -> {
            if (selectedRunId == null) {
                progressArea.setText("");
                finalRawArea.setText("");
                markdownPane.setText("");
                return;
            }
            TestRun run = runs.get(selectedRunId);
            if (run == null) return;
            synchronized (run) {
                run.buffer.setLength(0);
                run.progressBuffer.setLength(0);
                run.finalBuffer.setLength(0);
            }
            progressArea.setText("");
            finalRawArea.setText("");
            markdownPane.setText("");
        });

        copyButton.addActionListener(e -> {
            String text;
            int idx = rightTabs.getSelectedIndex();
            String tabName = idx >= 0 ? rightTabs.getTitleAt(idx) : "";
            if ("Request".equals(tabName)) {
                text = requestArea.getText();
            } else if ("Response".equals(tabName)) {
                text = responseArea.getText();
            } else {
                text = finalRawArea.getText();
            }
            Toolkit.getDefaultToolkit().getSystemClipboard().setContents(new StringSelection(text == null ? "" : text), null);
        });

        stopButton.addActionListener(e -> {
            if ("Cancel".equals(validateButton.getText())) {
                cancelValidateInProgress();
                return;
            }

            String runId = selectedRunId;
            if (runId != null && client.hasActiveRequest()) {
                client.cancelActiveRequest();
                appendProgressToRun(runId, "\n[info] Stream stopped.\n");
                setRunStatus(runId, "cancelled");
                return;
            }

            if (runId == null) return;
            TestRun run = runs.get(runId);
            if (run == null) return;

            String token = getToken();
            if (token == null || token.trim().isEmpty()) {
                appendProgressToRun(runId, "\n[error] Not validated.\n");
                return;
            }
            String convId;
            synchronized (run) {
                convId = run.conversationId;
            }
            if (convId == null || convId.trim().isEmpty()) {
                appendProgressToRun(runId, "\n[info] conversationId not available yet (wait for server to create session).\n");
                return;
            }

            stopButton.setEnabled(false);
            new Thread(() -> {
                try {
                    CyberStrikeAIClient.Config cfg = currentConfig();
                    client.cancelByConversationId(cfg.baseUrl, token, convId);
                    appendProgressToRun(runId, "\n[info] Cancel requested.\n");
                    setRunStatus(runId, "cancelling");
                } catch (Exception ex) {
                    appendProgressToRun(runId, "\n[error] Cancel failed: " + ex.getMessage() + "\n");
                } finally {
                    SwingUtilities.invokeLater(() -> stopButton.setEnabled(true));
                }
            }, "CyberStrikeAI-Cancel").start();
        });

        searchField.getDocument().addDocumentListener(new javax.swing.event.DocumentListener() {
            @Override public void insertUpdate(javax.swing.event.DocumentEvent e) { applyFilter(); }
            @Override public void removeUpdate(javax.swing.event.DocumentEvent e) { applyFilter(); }
            @Override public void changedUpdate(javax.swing.event.DocumentEvent e) { applyFilter(); }
        });

        renderMarkdownBox.addActionListener(e -> refreshOutputView());
    }

    CyberStrikeAIClient.Config currentConfig() {
        String host = hostField.getText().trim();
        String port = portField.getText().trim();
        String password = new String(passwordField.getPassword());
        String scheme = useHttpsBox.isSelected() ? "https" : "http";
        String baseUrl = scheme + "://" + host + ":" + port;
        return new CyberStrikeAIClient.Config(baseUrl, usernameField.getText(), password);
    }

    String getToken() {
        return tokenRef.get();
    }

    boolean isShowDebugEvents() {
        return showDebugEventsBox.isSelected();
    }

    private String nextRunId() {
        return "run_" + runSeq.getAndIncrement();
    }

    private String formatRunDisplay(String title, String agentMode, String status) {
        return title + " [" + agentMode + "] - " + status;
    }

    String startNewRun(String title, String agentMode, IHttpRequestResponse msg) {
        String id = nextRunId();
        TestRun run = new TestRun(id, title, agentMode);
        if (msg != null) {
            run.requestRaw = bytesToString(msg.getRequest());
            run.responseRaw = bytesToString(msg.getResponse());
        }
        runs.put(id, run);

        int index = testListModel.getSize();
        runIdToIndex.put(id, index);
        testListModel.addElement(run);
        filteredListModel.addElement(run);

        selectedRunId = id;
        filteredList.setSelectedIndex(filteredListModel.getSize() - 1);
        progressArea.setText("");
        finalRawArea.setText("");
        markdownPane.setText("");
        requestArea.setText(run.requestRaw);
        responseArea.setText(run.responseRaw);
        refreshOutputView();
        return id;
    }

    void setRunStatus(String runId, String status) {
        TestRun run = runs.get(runId);
        if (run == null) return;
        synchronized (run) {
            run.status = status;
        }
        Integer index = runIdToIndex.get(runId);
        if (index != null) {
            SwingUtilities.invokeLater(() -> filteredList.repaint());
        }
    }

    void setRunConversationId(String runId, String conversationId) {
        if (runId == null) return;
        TestRun run = runs.get(runId);
        if (run == null) return;
        synchronized (run) {
            run.conversationId = conversationId == null ? "" : conversationId;
        }
    }

    void appendToRun(String runId, String s) {
        // Backward compatibility: default to progress bucket
        appendProgressToRun(runId, s);
    }

    void appendProgressToRun(String runId, String s) {
        if (runId == null || s == null) return;
        TestRun run = runs.get(runId);
        if (run == null) return;
        synchronized (run) {
            run.buffer.append(s);
            run.progressBuffer.append(s);
        }
        if (runId.equals(selectedRunId)) {
            SwingUtilities.invokeLater(() -> appendProgressUi(s, false));
        }
    }

    private void appendProgressUi(String s, boolean forceFollow) {
        JScrollBar bar = progressScrollPane != null ? progressScrollPane.getVerticalScrollBar() : null;
        int scrollBefore = bar != null ? bar.getValue() : 0;
        boolean follow = forceFollow || isScrollNearBottom(progressScrollPane);
        progressArea.append(s);
        if (follow) {
            scrollPaneToBottom(progressScrollPane);
        } else if (bar != null) {
            bar.setValue(scrollBefore);
        }
    }

    private void appendFinalUi(String s, boolean forceFollow) {
        JScrollBar bar = finalRawScrollPane != null ? finalRawScrollPane.getVerticalScrollBar() : null;
        int scrollBefore = bar != null ? bar.getValue() : 0;
        boolean follow = forceFollow || isScrollNearBottom(finalRawScrollPane);
        finalRawArea.append(s);
        if (follow) {
            scrollPaneToBottom(finalRawScrollPane);
        } else if (bar != null) {
            bar.setValue(scrollBefore);
        }
    }

    void resetThinkingStream(String runId) {
        if (runId == null) return;
        TestRun run = runs.get(runId);
        if (run == null) return;
        synchronized (run) {
            run.thinkingPending.setLength(0);
        }
        appendProgressToRun(runId, "\n[thinking]\n");
    }

    void appendThinkingDelta(String runId, String delta) {
        if (runId == null || delta == null) return;
        TestRun run = runs.get(runId);
        if (run == null) return;

        StringBuilder toAppend = new StringBuilder();
        synchronized (run) {
            for (int i = 0; i < delta.length(); i++) {
                char c = delta.charAt(i);
                if (c == '\n') {
                    if (run.thinkingPending.length() > 0) {
                        toAppend.append("  ").append(run.thinkingPending).append("\n");
                        run.thinkingPending.setLength(0);
                    } else {
                        toAppend.append("\n");
                    }
                } else if (c != '\r') {
                    run.thinkingPending.append(c);
                }
            }
        }

        if (toAppend.length() > 0) {
            appendProgressToRun(runId, toAppend.toString());
        }
    }

    void appendFinalToRun(String runId, String s) {
        if (runId == null || s == null) return;
        TestRun run = runs.get(runId);
        if (run == null) return;
        synchronized (run) {
            run.buffer.append(s);
            run.finalBuffer.append(s);
        }
        if (runId.equals(selectedRunId)) {
            SwingUtilities.invokeLater(() -> appendFinalUi(s, false));
        }
    }

    void setFinalResponse(String runId, String finalResponse) {
        if (runId == null) return;
        TestRun run = runs.get(runId);
        if (run == null) return;
        synchronized (run) {
            run.finalResponse = finalResponse == null ? "" : finalResponse;
        }
        if (runId.equals(selectedRunId)) {
            SwingUtilities.invokeLater(this::refreshOutputView);
        }
    }

    private String getSelectedRunIdFromList() {
        TestRun run = filteredList.getSelectedValue();
        return run == null ? null : run.id;
    }

    private void setLogAreaToRun(String runId) {
        TestRun run = runs.get(runId);
        if (run == null) return;
        selectedRunId = runId;
        String progress;
        String fin;
        synchronized (run) {
            progress = run.progressBuffer.toString();
            fin = run.finalBuffer.toString();
        }
        SwingUtilities.invokeLater(() -> {
            progressArea.setText(progress);
            scrollPaneToBottom(progressScrollPane);
            finalRawArea.setText(fin);
            scrollPaneToBottom(finalRawScrollPane);
            requestArea.setText(run.requestRaw == null ? "" : run.requestRaw);
            responseArea.setText(run.responseRaw == null ? "" : run.responseRaw);
            refreshOutputView();
        });
    }

    private void clearAllRuns() {
        runs.clear();
        runIdToIndex.clear();
        testListModel.clear();
        filteredListModel.clear();
        selectedRunId = null;
        SwingUtilities.invokeLater(() -> {
            progressArea.setText("");
            finalRawArea.setText("");
            markdownPane.setText("");
            requestArea.setText("");
            responseArea.setText("");
        });
    }

    void clearAndShowStreamHeader(String title) {
        SwingUtilities.invokeLater(() -> {
            progressArea.setText("[*] " + title + "\n\n");
            finalRawArea.setText("");
            markdownPane.setText("");
        });
    }

    // Legacy helpers kept for Validate logging
    void appendStreamLine(String s) {
        if (s == null) return;
        SwingUtilities.invokeLater(() -> appendProgressUi(s + "\n", false));
    }

    private void log(String s) {
        appendStreamLine("[*] " + s);
    }

    private void cancelValidateInProgress() {
        client.cancelActiveRequest();
        Thread t = validateThreadRef.get();
        if (t != null) {
            t.interrupt();
        }
        SwingUtilities.invokeLater(() -> {
            statusLabel.setText("Cancelling...");
        });
        log("Validation cancelled.");
    }

    private void applyFilter() {
        String q = searchField.getText();
        if (q == null) q = "";
        String query = q.trim().toLowerCase();
        filteredListModel.clear();
        for (int i = 0; i < testListModel.size(); i++) {
            TestRun r = testListModel.getElementAt(i);
            if (query.isEmpty() || (r.title != null && r.title.toLowerCase().contains(query))) {
                filteredListModel.addElement(r);
            }
        }
        if (filteredListModel.size() > 0 && filteredList.getSelectedIndex() < 0) {
            filteredList.setSelectedIndex(0);
        }
    }

    private void refreshOutputView() {
        if (!renderMarkdownBox.isSelected()) {
            outputCardsLayout.show(outputCards, "raw");
            return;
        }

        if (selectedRunId == null) {
            outputCardsLayout.show(outputCards, "raw");
            return;
        }

        TestRun run = runs.get(selectedRunId);
        if (run == null) {
            outputCardsLayout.show(outputCards, "raw");
            return;
        }

        String finalResp;
        synchronized (run) {
            finalResp = run.finalResponse;
        }
        if (finalResp == null || finalResp.trim().isEmpty()) {
            // while streaming, stick to raw for performance
            outputCardsLayout.show(outputCards, "raw");
            return;
        }

        String html = MarkdownRenderer.toHtml(finalResp);
        markdownPane.setText(html);
        markdownPane.setCaretPosition(0);
        outputCardsLayout.show(outputCards, "md");
    }
    private static String bytesToString(byte[] bytes) {
        if (bytes == null || bytes.length == 0) return "";
        return new String(bytes, StandardCharsets.ISO_8859_1);
    }

    @Override
    public String getTabCaption() {
        return "CyberStrikeAI";
    }

    @Override
    public Component getUiComponent() {
        return root;
    }
}

