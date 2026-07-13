package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.InetAddress;
import java.net.Socket;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

// Loopback link to supervisor, retries and drops messages when unreachable
final class AgentConnection {
    private static final int MAX_FRAME_SIZE = 1 << 20;
    private static final long RECONNECT_DELAY_MS = 5000;
    private static final int QUEUE_CAPACITY = 256;

    private final int port;
    private final AgentProto.Hello hello;
    private final LinkedBlockingQueue<AgentProto.AgentMessage> queue =
            new LinkedBlockingQueue<AgentProto.AgentMessage>(QUEUE_CAPACITY);

    private volatile boolean running = true;
    private Thread thread;

    AgentConnection(int port, AgentProto.Hello hello) {
        this.port = port;
        this.hello = hello;
    }

    void start() {
        thread = new Thread(new Runnable() {
            @Override
            public void run() {
                connectLoop();
            }
        }, "disco-agent-io");
        thread.setDaemon(true);
        thread.start();
    }

    void stop() {
        running = false;
        if (thread != null) {
            thread.interrupt();
        }
    }

    void enqueue(AgentProto.AgentMessage message) {
        queue.offer(message);
    }

    private void connectLoop() {
        while (running) {
            try {
                runSession();
            } catch (IOException e) {
                // Supervisor away, retry quietly
            } catch (InterruptedException e) {
                return;
            }
            if (!running) {
                return;
            }
            try {
                Thread.sleep(RECONNECT_DELAY_MS);
            } catch (InterruptedException e) {
                return;
            }
        }
    }

    private void runSession() throws IOException, InterruptedException {
        Socket socket = new Socket(InetAddress.getByName("127.0.0.1"), port);
        try {
            socket.setTcpNoDelay(true);
            final DataOutputStream out = new DataOutputStream(socket.getOutputStream());
            final DataInputStream in = new DataInputStream(socket.getInputStream());

            writeFrame(out, AgentProto.AgentMessage.newBuilder().setHello(hello).build());

            // Reader drains inbound frames so socket close is noticed
            final Socket readerSocket = socket;
            Thread reader = new Thread(new Runnable() {
                @Override
                public void run() {
                    try {
                        readLoop(in);
                    } catch (IOException e) {
                        try {
                            readerSocket.close();
                        } catch (IOException ignored) {
                        }
                    }
                }
            }, "disco-agent-read");
            reader.setDaemon(true);
            reader.start();

            while (running) {
                AgentProto.AgentMessage message = queue.poll(1, TimeUnit.SECONDS);
                if (message != null) {
                    writeFrame(out, message);
                }
                if (socket.isClosed()) {
                    return;
                }
            }
        } finally {
            try {
                socket.close();
            } catch (IOException ignored) {
            }
        }
    }

    private void readLoop(DataInputStream in) throws IOException {
        while (running) {
            int length = in.readInt();
            if (length <= 0 || length > MAX_FRAME_SIZE) {
                throw new IOException("invalid frame length " + length);
            }
            byte[] data = new byte[length];
            in.readFully(data);
            // Nothing addresses the JVM agent, frames are drained
        }
    }

    private static void writeFrame(DataOutputStream out, AgentProto.AgentMessage message) throws IOException {
        byte[] data = message.toByteArray();
        synchronized (out) {
            out.writeInt(data.length);
            out.write(data);
            out.flush();
        }
    }
}
