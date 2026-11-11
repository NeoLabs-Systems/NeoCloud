import 'dart:math' as math;

/// One item a batch operation could not apply to.
class BatchFailure<T> {
  const BatchFailure(this.item, this.error);

  final T item;
  final Object error;
}

/// Runs [action] for every item, [concurrency] at a time, and returns the
/// items that failed in their original order. One failure never stops the rest.
Future<List<BatchFailure<T>>> runBatch<T>(Iterable<T> items, Future<void> Function(T item) action, {int concurrency = 6}) async {
  final queue = items.toList();
  final failures = <int, BatchFailure<T>>{};
  var next = 0;

  Future<void> worker() async {
    while (next < queue.length) {
      final index = next++;
      try {
        await action(queue[index]);
      } catch (error) {
        failures[index] = BatchFailure<T>(queue[index], error);
      }
    }
  }

  await Future.wait(<Future<void>>[for (var i = 0; i < math.min(concurrency, queue.length); i++) worker()]);
  final order = failures.keys.toList()..sort();
  return <BatchFailure<T>>[for (final index in order) failures[index]!];
}
