package com.collinpendleton.backhog.player

import com.google.common.util.concurrent.ListenableFuture
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.Executor
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicBoolean

/**
 * A settable [ListenableFuture], because media3's session callbacks speak
 * Guava futures while the app speaks coroutines — and the app does not ship
 * Guava for the sake of one factory method.
 *
 * Listeners run on the executor they were registered with; a future that is
 * already complete runs a new listener immediately. Cancellation is not a
 * path the session uses, but it is honoured for completeness.
 */
class SettableFuture<T> : ListenableFuture<T> {

    private val done = AtomicBoolean(false)
    @Volatile private var value: T? = null
    @Volatile private var failure: Throwable? = null
    private val listeners = CopyOnWriteArrayList<Pair<Runnable, Executor>>()

    override fun addListener(listener: Runnable, executor: Executor) {
        if (done.get()) {
            executor.execute(listener)
            return
        }
        listeners.add(listener to executor)
        if (done.get()) {
            listeners.remove(listener to executor)
            executor.execute(listener)
        }
    }

    override fun isDone(): Boolean = done.get()

    override fun get(): T {
        synchronized(this) {
            while (!done.get()) (this as Object).wait()
            failure?.let { throw it }
            @Suppress("UNCHECKED_CAST")
            return value as T
        }
    }

    override fun get(timeout: Long, unit: TimeUnit): T {
        val deadline = System.nanoTime() + unit.toNanos(timeout)
        synchronized(this) {
            while (!done.get()) {
                val left = deadline - System.nanoTime()
                if (left <= 0) throw TimeoutException("Future not set within $timeout $unit")
                TimeUnit.NANOSECONDS.timedWait(this, left)
            }
            failure?.let { throw it }
            @Suppress("UNCHECKED_CAST")
            return value as T
        }
    }

    override fun cancel(mayInterruptIfRunning: Boolean): Boolean = false

    override fun isCancelled(): Boolean = false

    private fun finish() {
        if (!done.compareAndSet(false, true)) return
        synchronized(this) { (this as Object).notifyAll() }
        listeners.forEach { (run, executor) -> executor.execute(run) }
    }

    fun set(value: T) {
        this.value = value
        finish()
    }

    fun setException(t: Throwable) {
        this.failure = t
        finish()
    }

    companion object {
        fun <T> immediate(value: T): ListenableFuture<T> =
            SettableFuture<T>().apply { set(value) }

        fun <T> failed(t: Throwable): ListenableFuture<T> =
            SettableFuture<T>().apply { setException(t) }
    }
}
