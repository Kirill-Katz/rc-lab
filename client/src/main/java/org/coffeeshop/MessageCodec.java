package org.coffeeshop;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.CharBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import com.fasterxml.jackson.core.JsonParser;

public final class MessageCodec {
    public static final int MAX_PAYLOAD = 65536;
    private final ObjectMapper mapper = new ObjectMapper();

    private static byte[] readFully(InputStream in, int length) throws IOException {
        byte[] b = new byte[length];
        int p = 0;
        while (p < length) {
            int n = in.read(b, p, length - p);
            if (n < 0) throw new EOFException("Unexpected end of frame");
            if (n == 0) continue;
            p += n;
        }
        return b;
    }

    public byte[] encode(JsonNode message) throws IOException {
        byte[] payload = mapper.writeValueAsBytes(message);
        if (payload.length == 0 || payload.length > MAX_PAYLOAD)
            throw new IOException("Invalid payload length: " + payload.length);
        return ByteBuffer.allocate(4 + payload.length).putInt(payload.length).put(payload).array();
    }

    public void write(OutputStream out, JsonNode message) throws IOException {
        byte[] frame = encode(message);
        out.write(frame);
        out.flush();
    }

    public JsonNode read(InputStream in) throws IOException {
        byte[] header = readFully(in, 4);
        int length = ByteBuffer.wrap(header).getInt();
        if (length < 1 || length > MAX_PAYLOAD)
            throw new IOException("Invalid frame length: " + Integer.toUnsignedLong(length));
        byte[] payload = readFully(in, length);
        String json = decodeUtf8(payload);
        try (JsonParser parser = mapper.createParser(json)) {
            JsonNode message = mapper.readTree(parser);
            if (message == null || !message.isObject()) throw new IOException("Message must be a JSON object");
            if (parser.nextToken() != null) throw new IOException("Message must contain exactly one JSON object");
            return message;
        }
    }

    private static String decodeUtf8(byte[] payload) throws IOException {
        try {
            CharBuffer chars = StandardCharsets.UTF_8.newDecoder()
                    .onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT)
                    .decode(ByteBuffer.wrap(payload));
            return chars.toString();
        } catch (CharacterCodingException e) {
            throw new IOException("Payload is not valid UTF-8", e);
        }
    }

    public ObjectMapper mapper() {
        return mapper;
    }
}
