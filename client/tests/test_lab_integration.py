#!/usr/bin/env python3
"""Black-box Tasks 1–4 checks using real Java CLI processes and the Go server.

Build first:
  (cd client && mvn -DskipTests package)
  (cd server && go build -o /tmp/coffee-server ./cmd)
Run:
  python3 client/tests/test_lab_integration.py --server /tmp/coffee-server

Only Python's standard library is needed. Each test starts an isolated server.
"""
import argparse
from pathlib import Path
import queue
import re
import socket
import subprocess
import threading
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]


class Process:
    def __init__(self, args):
        self.proc = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=subprocess.STDOUT, text=True, bufsize=1)
        self.lines = []
        self.events = queue.Queue()
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        for line in self.proc.stdout:
            self.lines.append(line.rstrip())
            self.events.put(line)

    def send(self, command):
        self.proc.stdin.write(command + "\n")
        self.proc.stdin.flush()

    def wait(self, pattern, timeout=5):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                line = self.events.get(timeout=max(.01, deadline - time.monotonic()))
            except queue.Empty:
                break
            match = re.search(pattern, line)
            if match:
                return match
        raise AssertionError(f"Timeout waiting for {pattern!r}:\n" + "\n".join(self.lines))

    def stop(self):
        if self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=3)
        self.reader.join(timeout=2)
        self.proc.stdin.close()
        self.proc.stdout.close()


class LabIntegrationTest(unittest.TestCase):
    def setUp(self):
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            self.port = probe.getsockname()[1]
        self.children = []
        self.server = Process([ARGS.server, "--host", "127.0.0.1", "--port", str(self.port)])
        self.addCleanup(self.server.stop)
        self.workers = int(self.server.wait(r"shop workers=(\d+)").group(1))
        deadline = time.monotonic() + 5
        while True:
            try:
                with socket.create_connection(("127.0.0.1", self.port), timeout=.2):
                    break
            except OSError:
                if time.monotonic() > deadline:
                    self.fail("Server did not start")
                time.sleep(.02)

    def client(self, role="client", authenticate=True):
        child = Process(["java", "-Xmx64m", "-jar", ARGS.jar, "--host", "127.0.0.1",
                         "--port", str(self.port), "--role", role])
        self.children.append(child)
        self.addCleanup(child.stop)
        if authenticate:
            child.wait("Authenticated as " + role)
        return child

    def stock(self, client):
        client.send("menu")
        result = {}
        for product, seconds in [("coffee", 4), ("tea", 3), ("croissant", 2)]:
            result[product] = int(client.wait(rf"{product} \| (\d+) \| {seconds}s").group(1))
        return result

    def accepted(self, client, seconds):
        return int(client.wait(rf"Order accepted: server order ID=(\d+), preparation={seconds}s").group(1))

    def test_task1_menu_preparation_and_ready_while_typing(self):
        client = self.client()
        stock = self.stock(client)
        self.assertTrue(all(10 <= q <= 50 for q in stock.values()))
        start = time.monotonic()
        client.send("order croissant=2 tea=1 coffee=1")
        oid = self.accepted(client, 11)
        self.assertLess(time.monotonic() - start, 2)
        current = self.stock(client)
        self.assertEqual(current, {"coffee": stock["coffee"] - 1, "tea": stock["tea"] - 1,
                                   "croissant": stock["croissant"] - 2})
        # Leave stdin open with an unfinished command: no newline or further request.
        client.proc.stdin.write("men")
        client.proc.stdin.flush()
        client.wait(rf"Order ready: server order ID={oid},", timeout=13)
        self.assertGreaterEqual(time.monotonic() - start, 10.8)
        client.proc.stdin.write("u\n")
        client.proc.stdin.flush()
        client.wait("Product \\| Available")
        client.send("history")
        client.wait(rf"{oid} \| ready \| 11s")

    def test_task1_refusal_is_atomic(self):
        client = self.client()
        before = self.stock(client)
        client.send(f"order coffee={before['coffee'] + 1} tea=1")
        client.wait("Order refused .*Insufficient stock.*Stock:")
        self.assertEqual(before, self.stock(client))

    def test_task2_multiple_outstanding_orders(self):
        client = self.client()
        client.send("order croissant=1")
        first = self.accepted(client, 2)
        client.send("order tea=1")
        second = self.accepted(client, 3)
        self.assertNotEqual(first, second)
        self.stock(client)
        client.wait(rf"Order ready: server order ID={first},", timeout=4)
        client.wait(rf"Order ready: server order ID={second},", timeout=4)
        client.send("history")
        # History order is unspecified; collect both without sequential matching.
        seen = {int(client.wait(r"(\d+) \| ready \| [23]s").group(1)) for _ in range(2)}
        self.assertEqual({first, second}, seen)

    def test_task2_more_clients_than_workers(self):
        provider = self.client("provider")
        provider.send(f"delivery coffee={self.workers + 1}")
        provider.wait("Delivery accepted")
        clients = [self.client() for _ in range(self.workers + 1)]
        start = time.monotonic()
        for child in clients:
            child.send("order coffee=1")
        ids = [self.accepted(child, 4) for child in clients]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertLess(time.monotonic() - start, 3)
        self.stock(clients[-1])  # usable even while its order is queued
        for child, oid in zip(clients, ids):
            child.wait(rf"Order ready: server order ID={oid},", timeout=10)
        self.assertGreaterEqual(time.monotonic() - start, 7.8)

    def test_task2_two_clients_compete_for_last_croissant(self):
        holder = self.client()
        initial = self.stock(holder)
        holder.send(f"order croissant={initial['croissant'] - 1}")
        self.accepted(holder, (initial["croissant"] - 1) * 2)
        contenders = [self.client(), self.client()]
        barrier = threading.Barrier(3)

        def order(child):
            barrier.wait(timeout=3)
            child.send("order croissant=1")

        threads = [threading.Thread(target=order, args=(child,)) for child in contenders]
        for thread in threads:
            thread.start()
        barrier.wait(timeout=3)
        for thread in threads:
            thread.join(timeout=3)
        outcomes = [child.wait(r"Order (accepted|refused)").group(1) for child in contenders]
        self.assertCountEqual(["accepted", "refused"], outcomes)
        self.assertEqual(0, self.stock(holder)["croissant"])

    def test_task3_delivery_alert_and_provider_reconnect(self):
        client = self.client()
        provider = self.client("provider")
        initial = self.stock(client)
        client.send(f"order coffee={initial['coffee'] - 4}")
        self.accepted(client, (initial["coffee"] - 4) * 4)
        provider.wait("LOW STOCK: coffee remaining=4")
        provider.send("delivery coffee=10 croissant=2")
        provider.wait("Delivery accepted")
        after = self.stock(client)
        self.assertEqual(14, after["coffee"])
        self.assertEqual(initial["croissant"] + 2, after["croissant"])
        provider.send("exit")
        provider.proc.wait(timeout=3)
        client.send("order coffee=10")
        self.accepted(client, 40)
        replacement = self.client("provider")
        replacement.wait("LOW STOCK: coffee remaining=4")
        self.assertEqual(4, self.stock(client)["coffee"])

    def test_task3_second_provider_rejected_cleanly(self):
        self.client("provider")
        other = self.client("provider", authenticate=False)
        other.wait("provider_exists")
        self.assertEqual(0, other.proc.wait(timeout=3))

    def test_task4_input_errors_and_role_restrictions(self):
        client = self.client()
        before = self.stock(client)
        for command, message in [
            ("nonsense", "Unknown command"),
            ("order", "Use: order"),
            ("order cocoa=1", "Use valid product"),
            ("order coffee=0", "Quantities must be integers"),
            ("order coffee=-1", "Quantities must be integers"),
            ("order coffee=1.5", "Quantities must be integers"),
            ("order coffee=1000001", "Quantities must be integers"),
            ("order coffee=abc", "Quantities must be integers"),
            ("delivery coffee=1", "not available for your role"),
        ]:
            client.send(command)
            client.wait(message)
        self.assertEqual(before, self.stock(client))
        provider = self.client("provider")
        provider.send("order tea=1")
        provider.wait("not available for your role")

    def test_task4_disconnect_refunds_active_and_queued_orders(self):
        observer = self.client()
        client = self.client()
        count = self.workers + 2
        # Each order takes >=40s, so none can finish before disconnection.
        provider = self.client("provider")
        provider.send(f"delivery coffee={count * 10}")
        provider.wait("Delivery accepted")
        before = self.stock(observer)
        for _ in range(count):
            client.send("order coffee=10")
            self.accepted(client, 40)
        self.assertEqual(before["coffee"] - count * 10, self.stock(observer)["coffee"])
        client.proc.kill()
        client.proc.wait(timeout=3)
        deadline = time.monotonic() + 3
        while self.stock(observer) != before:
            if time.monotonic() > deadline:
                self.fail("Orders were not fully refunded after client termination")
            time.sleep(.05)
        start = time.monotonic()
        observer.send("order croissant=1")
        oid = self.accepted(observer, 2)
        observer.wait(rf"Order ready: server order ID={oid},", timeout=3)
        self.assertGreaterEqual(time.monotonic() - start, 1.8)

    def test_task4_server_disappears_while_console_blocked(self):
        client = self.client()
        provider = self.client("provider")
        self.server.proc.kill()
        self.server.proc.wait(timeout=3)
        for child in [client, provider]:
            child.wait("Server connection closed")
            self.assertEqual(0, child.proc.wait(timeout=3))

    def test_task4_exit_and_console_eof(self):
        client = self.client()
        client.send("exit")
        self.assertEqual(0, client.proc.wait(timeout=3))
        provider = self.client("provider")
        provider.proc.stdin.close()
        self.assertEqual(0, provider.proc.wait(timeout=3))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", required=True, help="Path to compiled Go server")
    parser.add_argument("--jar", default=str(ROOT / "client/target/coffee-client-1.0.0.jar"))
    ARGS, unittest_args = parser.parse_known_args()
    unittest.main(argv=[__file__, *unittest_args], verbosity=2)
