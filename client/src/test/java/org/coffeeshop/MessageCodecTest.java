package org.coffeeshop;

import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import org.junit.jupiter.api.Test;

import java.io.*;

import static org.junit.jupiter.api.Assertions.*;

class MessageCodecTest {
    @Test
    void encodesBigEndianAndReadsSplitFrame() throws Exception {
        MessageCodec c = new MessageCodec();
        var n = JsonNodeFactory.instance.objectNode().put("text", "кофе");
        byte[] frame = c.encode(n);
        assertEquals(0, frame[0]);
        assertEquals(frame.length - 4, frame[3]);
        InputStream in = new InputStream() {
            int p;

            public int read(byte[] b, int o, int l) {
                if (p >= frame.length) return -1;
                b[o] = frame[p++];
                return 1;
            }

            public int read() {
                return p < frame.length ? frame[p++] & 255 : -1;
            }
        };
        assertEquals("кофе", c.read(in).path("text").asText());
    }

    @Test
    void readsMultipleFrames() throws Exception {
        MessageCodec c = new MessageCodec();
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        c.write(out, JsonNodeFactory.instance.objectNode().put("x", 1));
        c.write(out, JsonNodeFactory.instance.objectNode().put("x", 2));
        ByteArrayInputStream in = new ByteArrayInputStream(out.toByteArray());
        assertEquals(1, c.read(in).path("x").asInt());
        assertEquals(2, c.read(in).path("x").asInt());
    }

    @Test
    void rejectsInvalidLengths() {
        MessageCodec c = new MessageCodec();
        assertThrows(IOException.class, () -> c.read(new ByteArrayInputStream(new byte[]{0, 0, 0, 0})));
        assertThrows(IOException.class, () -> c.read(new ByteArrayInputStream(new byte[]{0, 1, 0, 0})));
    }

    @Test
    void rejectsInvalidUtf8ArraysAndTrailingJson() throws Exception {
        MessageCodec c = new MessageCodec();
        assertThrows(IOException.class, () -> c.read(frame(new byte[]{'{', '"', (byte) 0xc3, (byte) 0x28, '"', ':', '1', '}'})));
        assertThrows(IOException.class, () -> c.read(frame("[]".getBytes(java.nio.charset.StandardCharsets.UTF_8))));
        assertThrows(IOException.class, () -> c.read(frame("{} {}".getBytes(java.nio.charset.StandardCharsets.UTF_8))));
    }

    private static ByteArrayInputStream frame(byte[] payload) {
        java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
        out.write((payload.length >>> 24) & 255);
        out.write((payload.length >>> 16) & 255);
        out.write((payload.length >>> 8) & 255);
        out.write(payload.length & 255);
        out.writeBytes(payload);
        return new ByteArrayInputStream(out.toByteArray());
    }
}
